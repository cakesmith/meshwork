package dev.meshwork

import dev.meshwork.cli.MeshworkCli
import dev.meshwork.model.ExecutorKind
import dev.meshwork.runtime.MeshworkRuntime
import dev.meshwork.scenario.ScenarioRunner
import dev.meshwork.support.ScenarioAssertionException
import dev.meshwork.support.ValidationException
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.api.io.TempDir
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.PrintStream
import java.nio.file.Path
import kotlin.io.path.createDirectories
import kotlin.io.path.writeText
import kotlin.test.assertContains
import kotlin.test.assertEquals

class MeshworkScaffoldTest {
    @TempDir
    lateinit var tempDir: Path

    private val runtime = MeshworkRuntime()

    @Test
    fun `validate accepts a minimal external logic graph`() {
        val graphPath = writeTextGraph()

        val validated = runtime.loadValidatedGraph(graphPath, tempDir)

        assertEquals(2, validated.placeById.size)
        assertEquals(1, validated.transitionBindings.size)
        assertEquals(ExecutorKind.KOTLIN_PROCESS, validated.transitionBindings.single().executor.kind)
    }

    @Test
    fun `validate rejects absolute logic paths`() {
        val absoluteLogicPath = tempDir.resolve("logic").resolve("uppercase.kts").toString().replace("\\", "/")
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
                      path: "$absoluteLogicPath"
                arcs:
                  - from: source
                    to: uppercase
                    toPort: input
                  - from: uppercase
                    fromPort: output
                    to: sink
                """.trimIndent(),
            )

        assertThrows<ValidationException> {
            runtime.loadValidatedGraph(graphPath, tempDir)
        }
    }

    @Test
    fun `runtime executes text contract Kotlin logic`() {
        val graphPath = writeTextGraph()

        val result = runtime.runGraph(graphPath, tempDir, traceEnabled = true)

        assertEquals(1, result.stepsExecuted)
        assertEquals("HELLO", result.marking.getValue("sink").single().value.asText())
        assertContains(result.trace.single(), "transition=uppercase")
        assertContains(result.trace.single(), "durationMs=")
    }

    @Test
    fun `runtime executes json contract Kotlin logic`() {
        writeScript(
            "logic/annotate_json.kts",
            """
            import com.fasterxml.jackson.module.kotlin.jacksonObjectMapper

            fun run(input: String): String {
                val mapper = jacksonObjectMapper()
                val root = mapper.readTree(input)
                val message = root["inputs"]["message"].asText()
                return mapper.writeValueAsString(mapOf("outputs" to mapOf("message" to "json:${'$'}message")))
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
                  - id: source
                    type: STRING
                  - id: sink
                    type: STRING
                transitions:
                  - id: annotate
                    inputs:
                      - id: message
                        type: STRING
                    outputs:
                      - id: message
                        type: STRING
                    logic:
                      path: logic/annotate_json.kts
                arcs:
                  - from: source
                    to: annotate
                    toPort: message
                  - from: annotate
                    fromPort: message
                    to: sink
                initialMarking:
                  source:
                    - hello
                """.trimIndent(),
            )

        val result = runtime.runGraph(graphPath, tempDir)

        assertEquals("json:hello", result.marking.getValue("sink").single().value.asText())
    }

    @Test
    fun `runtime routes object return fields across fanout destinations`() {
        writeScript(
            "logic/fanout_object.kts",
            """
            fun run(input: String): Map<String, String> =
                mapOf(
                    "upperSink" to input.uppercase(),
                    "lowerSink" to input.lowercase(),
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
                  - id: upperSink
                    type: STRING
                  - id: lowerSink
                    type: STRING
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
                    to: upperSink
                  - from: split
                    fromPort: result
                    to: lowerSink
                initialMarking:
                  source:
                    - Hello Meshwork
                """.trimIndent(),
            )

        val result = runtime.runGraph(graphPath, tempDir)

        assertEquals("HELLO MESHWORK", result.marking.getValue("upperSink").single().value.asText())
        assertEquals("hello meshwork", result.marking.getValue("lowerSink").single().value.asText())
    }

    @Test
    fun `scenario runner passes and surfaces assertion failures`() {
        val graphPath = writeTextGraph()
        val scenarioPath =
            writeScenario(
                """
                graph: ${graphPath.fileName}
                mode: UNTIL_IDLE
                expect:
                  sink:
                    - HELLO
                """.trimIndent(),
            )

        val runner = ScenarioRunner(runtime)
        val success = runner.run(scenarioPath, tempDir)
        assertContains(success, "Scenario passed")

        val failingScenario =
            writeScenario(
                """
                graph: ${graphPath.fileName}
                mode: UNTIL_IDLE
                expect:
                  sink:
                    - no
                """.trimIndent(),
                fileName = "failing.yaml",
            )

        assertThrows<ScenarioAssertionException> {
            runner.run(failingScenario, tempDir)
        }
    }

    @Test
    fun `cli shows generated help for no args and command help`() {
        val noArgs = runCli()

        assertEquals(1, noArgs.exitCode)
        assertContains(noArgs.stdout, "meshwork")
        assertContains(noArgs.stdout, "run <graph.yaml> [options]")
        assertEquals("", noArgs.stderr)

        val runHelp = runCli("run", "--help")

        assertEquals(0, runHelp.exitCode)
        assertContains(runHelp.stdout, "--initial-file <path|->")
        assertContains(runHelp.stdout, "-h, --help")
        assertEquals("", runHelp.stderr)
    }

    @Test
    fun `cli smoke tests validate run and scenario`() {
        val graphPath = writeTextGraph()
        val scenarioPath =
            writeScenario(
                """
                graph: ${graphPath.fileName}
                mode: UNTIL_IDLE
                expect:
                  sink:
                    - HELLO
                """.trimIndent(),
            )

        val stdout = ByteArrayOutputStream()
        val stderr = ByteArrayOutputStream()

        System.setProperty("meshwork.projectRoot", tempDir.toString())
        val validateExit =
            try {
                MeshworkCli.run(arrayOf("validate", graphPath.fileName.toString()), PrintStream(stdout), PrintStream(stderr))
            } finally {
                System.clearProperty("meshwork.projectRoot")
            }
        System.setProperty("meshwork.projectRoot", tempDir.toString())
        val runExit =
            try {
                MeshworkCli.run(arrayOf("run", graphPath.fileName.toString(), "--trace"), PrintStream(stdout), PrintStream(stderr))
            } finally {
                System.clearProperty("meshwork.projectRoot")
            }
        System.setProperty("meshwork.projectRoot", tempDir.toString())
        val scenarioExit =
            try {
                MeshworkCli.run(arrayOf("scenario", scenarioPath.fileName.toString()), PrintStream(stdout), PrintStream(stderr))
            } finally {
                System.clearProperty("meshwork.projectRoot")
            }

        assertEquals(0, validateExit)
        assertEquals(0, runExit)
        assertEquals(0, scenarioExit)
        assertContains(stdout.toString(), "Validation succeeded")
        assertContains(stdout.toString(), "Run completed")
        assertContains(stdout.toString(), "Scenario passed")
        assertEquals("", stderr.toString())
    }

    @Test
    fun `cli run accepts initial override from file and stdin`() {
        val graphPath = writeTextGraph()
        val overrideFile =
            tempDir.resolve("override.yaml").apply {
                writeText(
                    """
                    source:
                      - from file
                    """.trimIndent(),
                )
            }

        val fileRun = runCli("run", graphPath.fileName.toString(), "--initial-file", overrideFile.fileName.toString())
        assertEquals(0, fileRun.exitCode)
        assertContains(fileRun.stdout, "FROM FILE")
        assertEquals("", fileRun.stderr)

        val stdinRun =
            runCli(
                "run",
                graphPath.fileName.toString(),
                "-i",
                "-",
                stdinText =
                    """
                    source:
                      - from stdin
                    """.trimIndent(),
            )
        assertEquals(0, stdinRun.exitCode)
        assertContains(stdinRun.stdout, "FROM STDIN")
        assertEquals("", stdinRun.stderr)
    }

    private fun writeTextGraph(): Path {
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
            initialMarking:
              source:
                - hello
            """.trimIndent(),
        )
    }

    private fun writeGraph(content: String, fileName: String = "graph.yaml"): Path {
        val path = tempDir.resolve(fileName)
        path.writeText(content)
        return path
    }

    private fun writeScenario(content: String, fileName: String = "scenario.yaml"): Path {
        val path = tempDir.resolve(fileName)
        path.writeText(content)
        return path
    }

    private fun runCli(vararg args: String, stdinText: String = ""): CliResult {
        val stdout = ByteArrayOutputStream()
        val stderr = ByteArrayOutputStream()
        System.setProperty("meshwork.projectRoot", tempDir.toString())
        val exitCode =
            try {
                MeshworkCli.run(
                    args = arrayOf(*args),
                    stdin = ByteArrayInputStream(stdinText.toByteArray()),
                    stdout = PrintStream(stdout),
                    stderr = PrintStream(stderr),
                )
            } finally {
                System.clearProperty("meshwork.projectRoot")
            }
        return CliResult(exitCode, stdout.toString(), stderr.toString())
    }

    private fun writeScript(relativePath: String, content: String) {
        val scriptPath = tempDir.resolve(relativePath)
        scriptPath.parent.createDirectories()
        scriptPath.writeText(content)
    }

    private data class CliResult(
        val exitCode: Int,
        val stdout: String,
        val stderr: String,
    )
}
