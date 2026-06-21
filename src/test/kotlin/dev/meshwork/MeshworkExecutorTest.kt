package dev.meshwork

import dev.meshwork.io.loadGraph
import dev.meshwork.runtime.MeshworkRuntime
import dev.meshwork.runtime.ServerRuntimeOptions
import dev.meshwork.support.JsonSupport
import dev.meshwork.support.MeshworkException
import dev.meshwork.support.ValidationException
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.api.io.TempDir
import java.io.ByteArrayOutputStream
import java.io.PrintStream
import java.net.URI
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse
import java.nio.file.Path
import kotlin.io.path.createDirectories
import kotlin.io.path.readText
import kotlin.io.path.writeText
import kotlin.test.assertContains
import kotlin.test.assertEquals
import kotlin.test.assertNotNull

class MeshworkExecutorTest {
    @TempDir
    lateinit var tempDir: Path

    private val runtime = MeshworkRuntime()

    @Test
    fun `parser reads executor wrapper config`() {
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: process-wrapper
                  executors:
                    - id: process-wrapper
                      kind: process
                      extensions: [".logic"]
                      wrapper:
                        command: ["powershell", "-ExecutionPolicy", "Bypass", "-File", "wrapper.ps1", "{logicPath}"]
                        mode: persistent
                        timeoutMs: 1234
                        handshake: ready
                places: []
                transitions: []
                arcs: []
                """.trimIndent(),
            )

        val graph = loadGraph(graphPath)
        val wrapper = graph.execution.executors.single().wrapper

        assertNotNull(wrapper)
        assertEquals(listOf("powershell", "-ExecutionPolicy", "Bypass", "-File", "wrapper.ps1", "{logicPath}"), wrapper.command)
        assertEquals(1234L, wrapper.timeoutMs)
        assertEquals("ready", wrapper.handshake)
    }

    @Test
    fun `validate rejects process executor without wrapper command`() {
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: process-wrapper
                  executors:
                    - id: process-wrapper
                      kind: process
                      extensions: [".logic"]
                      wrapper:
                        mode: oneshot
                        timeoutMs: 10
                places: []
                transitions: []
                arcs: []
                """.trimIndent(),
            )

        val error =
            assertThrows<ValidationException> {
                runtime.loadValidatedGraph(graphPath, tempDir)
            }

        assertContains(error.message ?: "", "wrapper.command must not be empty")
    }

    @Test
    fun `validate rejects non positive wrapper timeout`() {
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: process-wrapper
                  executors:
                    - id: process-wrapper
                      kind: process
                      extensions: [".logic"]
                      wrapper:
                        command: ["powershell", "-File", "wrapper.ps1"]
                        mode: oneshot
                        timeoutMs: 0
                places: []
                transitions: []
                arcs: []
                """.trimIndent(),
            )

        val error =
            assertThrows<ValidationException> {
                runtime.loadValidatedGraph(graphPath, tempDir)
            }

        assertContains(error.message ?: "", "wrapper.timeoutMs must be positive")
    }

    @Test
    fun `runtime executes generic oneshot process wrapper script`() {
        writePowerShellJsonWrapper("wrapper/echo_wrapper.ps1")
        writeLogicStub("logic/echo.logic")
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: process-wrapper
                  executors:
                    - id: process-wrapper
                      kind: process
                      extensions: [".logic"]
                      wrapper:
                        command: ["powershell", "-ExecutionPolicy", "Bypass", "-File", "wrapper/echo_wrapper.ps1", "{logicPath}"]
                        mode: oneshot
                        timeoutMs: 5000
                places:
                  - id: source
                    type: STRING
                  - id: sink
                    type: STRING
                transitions:
                  - id: echo
                    inputs:
                      - id: message
                        type: STRING
                    outputs:
                      - id: message
                        type: STRING
                    logic:
                      path: logic/echo.logic
                arcs:
                  - from: source
                    to: echo
                    toPort: message
                  - from: echo
                    fromPort: message
                    to: sink
                initialMarking:
                  source:
                    - hello process
                """.trimIndent(),
            )

        val result = runtime.runGraph(graphPath, tempDir)

        assertEquals("HELLO PROCESS", result.marking.getValue("sink").single().value.asText())
    }

    @Test
    fun `runtime reuses persistent process worker across multiple firings in one run`() {
        writeLogicStub("logic/persistent.logic")
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: persistent-process
                  executors:
                    - id: persistent-process
                      kind: process
                      extensions: [".logic"]
                      wrapper:
                        command: ["{java}", "-cp", "{classpath}", "dev.meshwork.testfixtures.PersistentCountingWorker"]
                        mode: persistent
                        timeoutMs: 5000
                places:
                  - id: source
                    type: STRING
                  - id: sink
                    type: STRING
                transitions:
                  - id: count
                    inputs:
                      - id: message
                        type: STRING
                    outputs:
                      - id: message
                        type: STRING
                    logic:
                      path: logic/persistent.logic
                arcs:
                  - from: source
                    to: count
                    toPort: message
                  - from: count
                    fromPort: message
                    to: sink
                initialMarking:
                  source:
                    - alpha
                    - beta
                """.trimIndent(),
            )

        val result = runtime.runGraph(graphPath, tempDir)
        val values = result.marking.getValue("sink").map { it.value.asText() }

        assertEquals(listOf("1:alpha", "2:beta"), values)
    }

    @Test
    fun `server session reuses persistent process worker across http events`() {
        writeLogicStub("logic/server.logic")
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: persistent-process
                  executors:
                    - id: persistent-process
                      kind: process
                      extensions: [".logic"]
                      wrapper:
                        command: ["{java}", "-cp", "{classpath}", "dev.meshwork.testfixtures.PersistentCountingWorker"]
                        mode: persistent
                        timeoutMs: 5000
                places:
                  - id: source
                    type: STRING
                    protocol:
                      kind: http
                      path: /ingest
                      awaitOutputs: [result]
                  - id: result
                    type: STRING
                    protocol:
                      kind: http
                transitions:
                  - id: count
                    inputs:
                      - id: message
                        type: STRING
                    outputs:
                      - id: message
                        type: STRING
                    logic:
                      path: logic/server.logic
                arcs:
                  - from: source
                    to: count
                    toPort: message
                  - from: count
                    fromPort: message
                    to: result
                """.trimIndent(),
            )
        val validated = runtime.loadValidatedGraph(graphPath, tempDir)

        runtime.openServerSession(
            validated,
            options = ServerRuntimeOptions(httpPort = 0),
            stdout = PrintStream(ByteArrayOutputStream()),
        ).use { session ->
            session.start()
            val port = session.boundHttpPort ?: error("missing port")
            val client = HttpClient.newHttpClient()

            val first =
                client.send(
                    HttpRequest.newBuilder(URI("http://127.0.0.1:$port/ingest"))
                        .POST(HttpRequest.BodyPublishers.ofString("first"))
                        .build(),
                    HttpResponse.BodyHandlers.ofString(),
                )
            val second =
                client.send(
                    HttpRequest.newBuilder(URI("http://127.0.0.1:$port/ingest"))
                        .POST(HttpRequest.BodyPublishers.ofString("second"))
                        .build(),
                    HttpResponse.BodyHandlers.ofString(),
                )

            assertEquals("1:first", JsonSupport.jsonMapper.readTree(first.body())["result"][0].asText())
            assertEquals("2:second", JsonSupport.jsonMapper.readTree(second.body())["result"][0].asText())
        }
    }

    @Test
    fun `runtime rejects mismatched response ids`() {
        writeLogicStub("logic/wrong.logic")
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: wrong-id
                  executors:
                    - id: wrong-id
                      kind: process
                      extensions: [".logic"]
                      wrapper:
                        command: ["{java}", "-cp", "{classpath}", "dev.meshwork.testfixtures.WrongIdWorker"]
                        mode: oneshot
                        timeoutMs: 5000
                places:
                  - id: source
                    type: STRING
                  - id: sink
                    type: STRING
                transitions:
                  - id: wrong
                    inputs:
                      - id: message
                        type: STRING
                    outputs:
                      - id: message
                        type: STRING
                    logic:
                      path: logic/wrong.logic
                arcs:
                  - from: source
                    to: wrong
                    toPort: message
                  - from: wrong
                    fromPort: message
                    to: sink
                initialMarking:
                  source:
                    - hi
                """.trimIndent(),
            )

        val error =
            assertThrows<MeshworkException> {
                runtime.runGraph(graphPath, tempDir)
            }

        assertContains(error.message ?: "", "response id mismatch")
    }

    @Test
    fun `runtime times out hung process worker`() {
        writeLogicStub("logic/sleep.logic")
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: sleepy
                  executors:
                    - id: sleepy
                      kind: process
                      extensions: [".logic"]
                      wrapper:
                        command: ["{java}", "-cp", "{classpath}", "dev.meshwork.testfixtures.SleepingWorker", "200"]
                        mode: oneshot
                        timeoutMs: 50
                places:
                  - id: source
                    type: STRING
                  - id: sink
                    type: STRING
                transitions:
                  - id: nap
                    inputs:
                      - id: message
                        type: STRING
                    outputs:
                      - id: message
                        type: STRING
                    logic:
                      path: logic/sleep.logic
                arcs:
                  - from: source
                    to: nap
                    toPort: message
                  - from: nap
                    fromPort: message
                    to: sink
                initialMarking:
                  source:
                    - hi
                """.trimIndent(),
            )

        val error =
            assertThrows<MeshworkException> {
                runtime.runGraph(graphPath, tempDir)
            }

        assertContains(error.message ?: "", "timed out")
    }

    @Test
    fun `runtime executes compiled wrapper through generic process adapter`() {
        writeLogicStub("logic/compiled.logic")
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: compiled
                  executors:
                    - id: compiled
                      kind: process
                      extensions: [".logic"]
                      wrapper:
                        command: ["{java}", "-cp", "{classpath}", "dev.meshwork.testfixtures.CompiledEchoWorker"]
                        mode: oneshot
                        timeoutMs: 5000
                places:
                  - id: source
                    type: STRING
                  - id: sink
                    type: STRING
                transitions:
                  - id: compiled
                    inputs:
                      - id: message
                        type: STRING
                    outputs:
                      - id: message
                        type: STRING
                    logic:
                      path: logic/compiled.logic
                arcs:
                  - from: source
                    to: compiled
                    toPort: message
                  - from: compiled
                    fromPort: message
                    to: sink
                initialMarking:
                  source:
                    - compiled path
                """.trimIndent(),
            )

        val result = runtime.runGraph(graphPath, tempDir)

        assertEquals("COMPILED PATH", result.marking.getValue("sink").single().value.asText())
    }

    @Test
    fun `runtime writes flamegraph samples from metrics plugin`() {
        writePowerShellJsonWrapper("wrapper/echo_wrapper.ps1")
        writeLogicStub("logic/flame.logic")
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: process-wrapper
                  metrics:
                    plugins:
                      - kind: flamegraph
                        output: build/metrics/transition.folded
                  executors:
                    - id: process-wrapper
                      kind: process
                      extensions: [".logic"]
                      wrapper:
                        command: ["powershell", "-ExecutionPolicy", "Bypass", "-File", "wrapper/echo_wrapper.ps1", "{logicPath}"]
                        mode: oneshot
                        timeoutMs: 5000
                places:
                  - id: source
                    type: STRING
                  - id: sink
                    type: STRING
                transitions:
                  - id: echo
                    inputs:
                      - id: message
                        type: STRING
                    outputs:
                      - id: message
                        type: STRING
                    logic:
                      path: logic/flame.logic
                arcs:
                  - from: source
                    to: echo
                    toPort: message
                  - from: echo
                    fromPort: message
                    to: sink
                initialMarking:
                  source:
                    - flame
                """.trimIndent(),
            )

        runtime.runGraph(graphPath, tempDir)

        val flamegraph = tempDir.resolve("build/metrics/transition.folded").readText()
        assertContains(flamegraph, "meshwork;echo;ok ")
    }

    @Test
    fun `server exposes prometheus metrics plugin`() {
        writeLogicStub("logic/metrics.logic")
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: persistent-process
                  metrics:
                    plugins:
                      - kind: prometheus
                        path: /metrics
                  executors:
                    - id: persistent-process
                      kind: process
                      extensions: [".logic"]
                      wrapper:
                        command: ["{java}", "-cp", "{classpath}", "dev.meshwork.testfixtures.PersistentCountingWorker"]
                        mode: persistent
                        timeoutMs: 5000
                places:
                  - id: source
                    type: STRING
                    protocol:
                      kind: http
                      path: /ingest
                      awaitOutputs: [result]
                  - id: result
                    type: STRING
                    protocol:
                      kind: http
                transitions:
                  - id: count
                    inputs:
                      - id: message
                        type: STRING
                    outputs:
                      - id: message
                        type: STRING
                    logic:
                      path: logic/metrics.logic
                arcs:
                  - from: source
                    to: count
                    toPort: message
                  - from: count
                    fromPort: message
                    to: result
                """.trimIndent(),
            )
        val validated = runtime.loadValidatedGraph(graphPath, tempDir)

        runtime.openServerSession(
            validated,
            options = ServerRuntimeOptions(httpPort = 0),
            stdout = PrintStream(ByteArrayOutputStream()),
        ).use { session ->
            session.start()
            val port = session.boundHttpPort ?: error("missing port")
            val client = HttpClient.newHttpClient()

            client.send(
                HttpRequest.newBuilder(URI("http://127.0.0.1:$port/ingest"))
                    .POST(HttpRequest.BodyPublishers.ofString("first"))
                    .build(),
                HttpResponse.BodyHandlers.ofString(),
            )
            val metricsResponse =
                client.send(
                    HttpRequest.newBuilder(URI("http://127.0.0.1:$port/metrics"))
                        .GET()
                        .build(),
                    HttpResponse.BodyHandlers.ofString(),
                )

            assertEquals(200, metricsResponse.statusCode())
            assertContains(metricsResponse.body(), "meshwork_transition_duration_seconds_count")
            assertContains(metricsResponse.body(), "transition=\"count\"")
            assertContains(metricsResponse.body(), "executor=\"persistent-process\"")
        }
    }

    private fun writeGraph(content: String, fileName: String = "graph.yaml"): Path {
        val path = tempDir.resolve(fileName)
        path.writeText(content)
        return path
    }

    private fun writeLogicStub(relativePath: String) {
        val logicPath = tempDir.resolve(relativePath)
        logicPath.parent.createDirectories()
        logicPath.writeText("stub")
    }

    private fun writePowerShellJsonWrapper(relativePath: String) {
        val wrapperPath = tempDir.resolve(relativePath)
        wrapperPath.parent.createDirectories()
        wrapperPath.writeText(
            """
            param([string]${'$'}logicPath)
            ${'$'}request = [Console]::In.ReadLine() | ConvertFrom-Json
            ${'$'}response = @{
              id = ${'$'}request.id
              outputs = @{
                message = ${'$'}request.inputs.message.ToUpperInvariant()
              }
            } | ConvertTo-Json -Compress
            [Console]::Out.WriteLine(${'$'}response)
            """.trimIndent(),
        )
    }
}
