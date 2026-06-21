package dev.meshwork

import dev.meshwork.cli.MeshworkCli
import dev.meshwork.io.loadGraph
import dev.meshwork.runtime.MeshworkRuntime
import dev.meshwork.runtime.ServerRuntimeOptions
import dev.meshwork.runtime.resolveEffectivePlaceProtocols
import dev.meshwork.support.JsonSupport
import dev.meshwork.support.ValidationException
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.api.io.TempDir
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.PrintStream
import java.net.URI
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse
import java.nio.file.Path
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import kotlin.io.path.createDirectories
import kotlin.io.path.writeText
import kotlin.test.assertContains
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

class MeshworkServerTest {
    @TempDir
    lateinit var tempDir: Path

    private val runtime = MeshworkRuntime()

    @Test
    fun `parser reads place protocol config`() {
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: kotlin-script
                  executors:
                    - id: kotlin-script
                      kind: kotlin-process
                      extensions: [".kts"]
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
                transitions: []
                arcs: []
                """.trimIndent(),
            )

        val graph = loadGraph(graphPath)
        val protocol = graph.places.first().protocol

        assertNotNull(protocol)
        assertEquals("/ingest", protocol.path)
        assertEquals(listOf("result"), protocol.awaitOutputs)
    }

    @Test
    fun `validate rejects duplicate http paths and unknown awaited outputs`() {
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: kotlin-script
                  executors:
                    - id: kotlin-script
                      kind: kotlin-process
                      extensions: [".kts"]
                places:
                  - id: source
                    type: STRING
                    protocol:
                      kind: http
                      path: /shared
                      awaitOutputs: [missing]
                  - id: result
                    type: STRING
                    protocol:
                      kind: http
                      path: /shared
                transitions: []
                arcs: []
                """.trimIndent(),
            )

        val error =
            assertThrows<ValidationException> {
                runtime.loadValidatedGraph(graphPath, tempDir)
            }

        assertContains(error.message ?: "", "share HTTP path '/shared'")
        assertContains(error.message ?: "", "references unknown place 'missing'")
    }

    @Test
    fun `runtime infers stdin and stdout boundary protocols`() {
        val graphPath = writeServerUppercaseGraph()
        val validated = runtime.loadValidatedGraph(graphPath, tempDir)

        val protocols = resolveEffectivePlaceProtocols(validated)

        assertEquals("STDIN", protocols.getValue("source").kind.name)
        assertEquals("STDOUT", protocols.getValue("sink").kind.name)
    }

    @Test
    fun `cli run server http overlay lists http places`() {
        val graphPath = writeServerUppercaseGraph()
        val stdout = ByteArrayOutputStream()
        val stderr = ByteArrayOutputStream()
        val exitCode = AtomicInteger(-1)
        val finished = CountDownLatch(1)

        System.setProperty("meshwork.projectRoot", tempDir.toString())
        val worker =
            Thread {
                try {
                    exitCode.set(
                        MeshworkCli.run(
                            args = arrayOf("run", graphPath.fileName.toString(), "--server", "--http", "--http-port", "0"),
                            stdin = ByteArrayInputStream(ByteArray(0)),
                            stdout = PrintStream(stdout),
                            stderr = PrintStream(stderr),
                        ),
                    )
                } finally {
                    finished.countDown()
                }
            }

        worker.start()
        waitForText(stdout, "HTTP server listening on")
        worker.interrupt()
        finished.await(5, TimeUnit.SECONDS)
        System.clearProperty("meshwork.projectRoot")

        assertContains(stdout.toString(), "source: http")
        assertContains(stdout.toString(), "sink: http")
        assertEquals(130, exitCode.get())
        assertEquals("", stderr.toString())
    }

    @Test
    fun `server session processes single stdin place and drains stdout`() {
        val graphPath = writeServerUppercaseGraph()
        val validated = runtime.loadValidatedGraph(graphPath, tempDir)
        val stdout = ByteArrayOutputStream()

        runtime.openServerSession(validated, stdout = PrintStream(stdout)).use { session ->
            session.processStdinLine("hello meshwork")
        }

        assertEquals("HELLO MESHWORK", stdout.toString().trim())
    }

    @Test
    fun `server session accepts multi-place stdin json events`() {
        writeScript(
            "logic/join_json.kts",
            """
            import com.fasterxml.jackson.module.kotlin.jacksonObjectMapper

            fun run(input: String): String {
                val mapper = jacksonObjectMapper()
                val root = mapper.readTree(input)
                val left = root["inputs"]["left"].asText()
                val right = root["inputs"]["right"].asText()
                return mapper.writeValueAsString(mapOf("outputs" to mapOf("message" to "${'$'}left|${'$'}right")))
            }
            """.trimIndent(),
        )
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: kotlin-script
                  executors:
                    - id: kotlin-script
                      kind: kotlin-process
                      extensions: [".kts"]
                      contract: JSON
                places:
                  - id: leftSource
                    type: STRING
                  - id: rightSource
                    type: STRING
                  - id: sink
                    type: STRING
                transitions:
                  - id: join
                    inputs:
                      - id: left
                        type: STRING
                      - id: right
                        type: STRING
                    outputs:
                      - id: message
                        type: STRING
                    logic:
                      path: logic/join_json.kts
                arcs:
                  - from: leftSource
                    to: join
                    toPort: left
                  - from: rightSource
                    to: join
                    toPort: right
                  - from: join
                    fromPort: message
                    to: sink
                """.trimIndent(),
            )
        val validated = runtime.loadValidatedGraph(graphPath, tempDir)
        val stdout = ByteArrayOutputStream()

        runtime.openServerSession(validated, stdout = PrintStream(stdout)).use { session ->
            session.processStdinLine("""{"leftSource":["alpha"],"rightSource":["beta"]}""")
        }

        assertEquals("alpha|beta", stdout.toString().trim())
    }

    @Test
    fun `http async post returns accepted and get drains outputs`() {
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: kotlin-script
                  executors:
                    - id: kotlin-script
                      kind: kotlin-process
                      extensions: [".kts"]
                      contract: TEXT
                places:
                  - id: source
                    type: STRING
                    protocol:
                      kind: http
                      path: /ingest
                  - id: result
                    type: STRING
                    protocol:
                      kind: http
                      path: /result
                transitions:
                  - id: uppercase
                    inputs:
                      - id: input
                        type: STRING
                    outputs:
                      - id: output
                        type: STRING
                    logic:
                      path: logic/uppercase.kts
                arcs:
                  - from: source
                    to: uppercase
                    toPort: input
                  - from: uppercase
                    fromPort: output
                    to: result
                """.trimIndent(),
            )
        writeScript("logic/uppercase.kts", "fun run(input: String): String = input.uppercase()")
        val validated = runtime.loadValidatedGraph(graphPath, tempDir)

        runtime.openServerSession(
            validated,
            options = ServerRuntimeOptions(httpPort = 0),
            stdout = PrintStream(ByteArrayOutputStream()),
        ).use { session ->
            session.start()
            val port = session.boundHttpPort ?: error("missing port")
            val client = HttpClient.newHttpClient()

            val post =
                client.send(
                    HttpRequest.newBuilder(URI("http://127.0.0.1:$port/ingest"))
                        .POST(HttpRequest.BodyPublishers.ofString("hello"))
                        .build(),
                    HttpResponse.BodyHandlers.ofString(),
                )
            val get =
                client.send(
                    HttpRequest.newBuilder(URI("http://127.0.0.1:$port/result"))
                        .GET()
                        .build(),
                    HttpResponse.BodyHandlers.ofString(),
                )

            assertEquals(202, post.statusCode())
            assertEquals("""{"result":["HELLO"]}""", get.body())
        }
    }

    @Test
    fun `http sync post returns configured awaited outputs`() {
        writeScript(
            "logic/fanout_object.kts",
            """
            fun run(input: String): Map<String, String> =
                mapOf(
                    "upperResult" to input.uppercase(),
                    "lowerResult" to input.lowercase(),
                )
            """.trimIndent(),
        )
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: kotlin-script
                  executors:
                    - id: kotlin-script
                      kind: kotlin-process
                      extensions: [".kts"]
                      contract: TEXT
                places:
                  - id: source
                    type: STRING
                    protocol:
                      kind: http
                      path: /split
                      awaitOutputs: [upperResult, lowerResult]
                  - id: upperResult
                    type: STRING
                    protocol:
                      kind: http
                  - id: lowerResult
                    type: STRING
                    protocol:
                      kind: http
                transitions:
                  - id: split
                    inputs:
                      - id: input
                        type: STRING
                    outputs:
                      - id: result
                        type: STRING
                    logic:
                      path: logic/fanout_object.kts
                arcs:
                  - from: source
                    to: split
                    toPort: input
                  - from: split
                    fromPort: result
                    to: upperResult
                  - from: split
                    fromPort: result
                    to: lowerResult
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
            val response =
                client.send(
                    HttpRequest.newBuilder(URI("http://127.0.0.1:$port/split"))
                        .POST(HttpRequest.BodyPublishers.ofString("Hello Meshwork"))
                        .build(),
                    HttpResponse.BodyHandlers.ofString(),
                )

            assertEquals(200, response.statusCode())
            val body = JsonSupport.jsonMapper.readTree(response.body())
            assertEquals("HELLO MESHWORK", body["upperResult"][0].asText())
            assertEquals("hello meshwork", body["lowerResult"][0].asText())
        }
    }

    @Test
    fun `http awaited output timeout returns 504`() {
        val graphPath =
            writeGraph(
                """
                execution:
                  defaultExecutor: kotlin-script
                  executors:
                    - id: kotlin-script
                      kind: kotlin-process
                      extensions: [".kts"]
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
                transitions: []
                arcs: []
                """.trimIndent(),
            )
        val validated = runtime.loadValidatedGraph(graphPath, tempDir)

        runtime.openServerSession(
            validated,
            options = ServerRuntimeOptions(httpPort = 0, timeoutMs = 50),
            stdout = PrintStream(ByteArrayOutputStream()),
        ).use { session ->
            session.start()
            val port = session.boundHttpPort ?: error("missing port")
            val client = HttpClient.newHttpClient()
            val response =
                client.send(
                    HttpRequest.newBuilder(URI("http://127.0.0.1:$port/ingest"))
                        .POST(HttpRequest.BodyPublishers.ofString("hello"))
                        .build(),
                    HttpResponse.BodyHandlers.ofString(),
                )

            assertEquals(504, response.statusCode())
            assertTrue(response.body().contains("pending"))
            assertTrue(response.body().contains("result"))
        }
    }

    private fun writeServerUppercaseGraph(): Path {
        writeScript("logic/uppercase.kts", "fun run(input: String): String = input.uppercase()")
        return writeGraph(
            """
            execution:
              defaultExecutor: kotlin-script
              executors:
                - id: kotlin-script
                  kind: kotlin-process
                  extensions: [".kts"]
                  contract: TEXT
            places:
              - id: source
                type: STRING
              - id: sink
                type: STRING
            transitions:
              - id: uppercase
                inputs:
                  - id: input
                    type: STRING
                outputs:
                  - id: output
                    type: STRING
                logic:
                  path: logic/uppercase.kts
            arcs:
              - from: source
                to: uppercase
                toPort: input
              - from: uppercase
                fromPort: output
                to: sink
            """.trimIndent(),
        )
    }

    private fun waitForText(stdout: ByteArrayOutputStream, needle: String) {
        repeat(100) {
            if (stdout.toString().contains(needle)) {
                return
            }
            Thread.sleep(20)
        }
        error("Timed out waiting for '$needle' in output: ${stdout.toString()}")
    }

    private fun writeGraph(content: String, fileName: String = "graph.yaml"): Path {
        val path = tempDir.resolve(fileName)
        path.writeText(content)
        return path
    }

    private fun writeScript(relativePath: String, content: String) {
        val scriptPath = tempDir.resolve(relativePath)
        scriptPath.parent.createDirectories()
        scriptPath.writeText(content)
    }
}
