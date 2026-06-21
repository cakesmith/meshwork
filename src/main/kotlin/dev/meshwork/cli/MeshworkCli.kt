package dev.meshwork.cli

import dev.meshwork.io.parseInitialMarking
import dev.meshwork.runtime.MeshworkRuntime
import dev.meshwork.runtime.ServerRuntimeOptions
import dev.meshwork.scenario.ScenarioRunner
import dev.meshwork.support.JsonSupport
import dev.meshwork.support.MeshworkException
import java.io.InputStream
import java.io.PrintStream
import java.nio.charset.StandardCharsets
import java.nio.file.Path
import kotlin.io.path.readText
import kotlin.system.exitProcess

fun main(args: Array<String>) {
    exitProcess(MeshworkCli.run(args, System.`in`, System.out, System.err))
}

object MeshworkCli {
    private val runtime = MeshworkRuntime()
    private val scenarioRunner = ScenarioRunner(runtime)
    private val parser =
        CliParser(
            programName = "meshwork",
            summary = "Minimal Petri-net validation, simulation, and scenario runner.",
            commands =
                listOf(
                    CliCommandSpec(
                        name = "validate",
                        summary = "Validate a graph file.",
                        positionals = listOf(CliPositionalSpec("graph.yaml", "Repo-relative graph file to validate.")),
                    ),
                    CliCommandSpec(
                        name = "run",
                        summary = "Run a graph simulation.",
                        positionals = listOf(CliPositionalSpec("graph.yaml", "Repo-relative graph file to run.")),
                        options =
                            listOf(
                                CliOptionSpec("--steps", valueName = "<N>", description = "Stop after at most N steps."),
                                CliOptionSpec("--trace", description = "Print executed transitions."),
                                CliOptionSpec("--server", description = "Keep the process open and serve place protocols."),
                                CliOptionSpec("--flamegraph", valueName = "<path>", description = "Write folded flamegraph samples to this repo-relative file."),
                                CliOptionSpec("--http", shortName = "-http", description = "Resolve every unconfigured place to HTTP."),
                                CliOptionSpec("--http-host", valueName = "<host>", description = "Bind the HTTP server to this host."),
                                CliOptionSpec("--http-port", valueName = "<port>", description = "Bind the HTTP server to this port."),
                                CliOptionSpec("--timeout-ms", valueName = "<N>", description = "Timeout for awaited HTTP outputs."),
                                CliOptionSpec(
                                    "--initial-file",
                                    shortName = "-i",
                                    valueName = "<path|->",
                                    description = "Load an initial marking override from YAML/JSON file or stdin.",
                                ),
                            ),
                    ),
                    CliCommandSpec(
                        name = "scenario",
                        summary = "Run a scenario file with trace output.",
                        positionals = listOf(CliPositionalSpec("scenario.yaml", "Repo-relative scenario file to run.")),
                    ),
                ),
        )

    fun run(
        args: Array<String>,
        stdout: PrintStream,
        stderr: PrintStream,
    ): Int = run(args, System.`in`, stdout, stderr)

    fun run(
        args: Array<String>,
        stdin: InputStream,
        stdout: PrintStream,
        stderr: PrintStream,
    ): Int =
        try {
            when (val parsed = parser.parse(args)) {
                is CliParseResult.Help -> {
                    stdout.println(parsed.text)
                    parsed.exitCode
                }
                is CliParseResult.Command -> execute(parsed.parsed, stdin, stdout)
            }
        } catch (error: MeshworkException) {
            stderr.println(error.message)
            1
        } catch (error: IllegalArgumentException) {
            stderr.println(error.message)
            1
        } catch (error: InterruptedException) {
            Thread.currentThread().interrupt()
            130
        }

    private fun execute(
        parsed: ParsedCliCommand,
        stdin: InputStream,
        stdout: PrintStream,
    ): Int =
        when (parsed.command.name) {
            "validate" -> validate(parsed, stdout)
            "run" -> runGraph(parsed, stdin, stdout)
            "scenario" -> runScenario(parsed, stdout)
            else -> throw IllegalArgumentException("Unknown command '${parsed.command.name}'.")
        }

    private fun validate(parsed: ParsedCliCommand, stdout: PrintStream): Int {
        val graphPath = resolveRepoRelative(parsed.positional("graph.yaml"))
        val validated = runtime.loadValidatedGraph(graphPath, projectRoot())
        stdout.println("Validation succeeded for ${validated.graphPath.fileName}.")
        stdout.println("places=${validated.placeById.size} transitions=${validated.transitionBindings.size}")
        return 0
    }

    private fun runGraph(
        parsed: ParsedCliCommand,
        stdin: InputStream,
        stdout: PrintStream,
    ): Int {
        val graphPath = resolveRepoRelative(parsed.positional("graph.yaml"))
        val stepLimit = parsed.optionValue("--steps")?.toInt()
        val traceEnabled = parsed.hasOption("--trace")
        val flamegraphOutput = parsed.optionValue("--flamegraph")?.let(::resolveRepoRelative)
        val initialOverride =
            parsed.optionValue("--initial-file")
                ?.let { loadInitialOverride(it, stdin) }
                .orEmpty()

        if (parsed.hasOption("--server")) {
            return runServer(graphPath, stdin, stdout, parsed, initialOverride, flamegraphOutput)
        }

        val result =
            runtime.runGraph(
                graphPath,
                projectRoot(),
                stepLimit = stepLimit,
                initialOverride = initialOverride,
                traceEnabled = traceEnabled,
                flamegraphOutput = flamegraphOutput,
            )
        stdout.println("Run completed: steps=${result.stepsExecuted}, idle=${result.terminatedBecauseIdle}")
        if (traceEnabled && result.trace.isNotEmpty()) {
            result.trace.forEach(stdout::println)
        }
        stdout.println(renderMarking(result.marking))
        flamegraphOutput?.let { stdout.println("Flamegraph samples written to ${projectRoot().relativize(it)}") }
        return 0
    }

    private fun runServer(
        graphPath: Path,
        stdin: InputStream,
        stdout: PrintStream,
        parsed: ParsedCliCommand,
        initialOverride: Map<String, List<com.fasterxml.jackson.databind.JsonNode>>,
        flamegraphOutput: Path?,
    ): Int {
        val options =
            ServerRuntimeOptions(
                enableHttpOverlay = parsed.hasOption("--http"),
                httpHost = parsed.optionValue("--http-host") ?: "127.0.0.1",
                httpPort = parsed.optionValue("--http-port")?.toInt() ?: 8080,
                timeoutMs = parsed.optionValue("--timeout-ms")?.toLong() ?: 30000L,
            )

        runtime.openServerSession(
            graphPath = graphPath,
            projectRoot = projectRoot(),
            options = options,
            initialOverride = initialOverride,
            stdout = stdout,
            flamegraphOutput = flamegraphOutput,
        ).use { session ->
            session.start()
            stdout.println("Server mode started.")
            session.protocolSummary().values.sortedBy { it.place.id }.forEach { protocol ->
                val details =
                    when (protocol.kind) {
                        dev.meshwork.model.PlaceProtocolKind.HTTP -> " path=${protocol.path}"
                        else -> ""
                    }
                stdout.println("${protocol.place.id}: ${protocol.kind.name.lowercase()}$details")
            }
            session.boundHttpPort?.let { port ->
                stdout.println("HTTP server listening on http://${options.httpHost}:$port")
            }
            session.metricsPaths().forEach { path ->
                stdout.println("metrics: http://${options.httpHost}:${session.boundHttpPort ?: options.httpPort}$path")
            }
            stdout.flush()

            session.runStdinLoop(stdin)
            if (session.hasHttpServer) {
                session.awaitShutdown()
            }
        }
        flamegraphOutput?.let { stdout.println("Flamegraph samples written to ${projectRoot().relativize(it)}") }
        return 0
    }

    private fun runScenario(parsed: ParsedCliCommand, stdout: PrintStream): Int {
        val scenarioPath = resolveRepoRelative(parsed.positional("scenario.yaml"))
        stdout.println(scenarioRunner.run(scenarioPath, projectRoot(), traceEnabled = true))
        return 0
    }

    private fun loadInitialOverride(
        rawPath: String,
        stdin: InputStream,
    ) = parseInitialMarking(readInitialOverrideContent(rawPath, stdin))

    private fun readInitialOverrideContent(
        rawPath: String,
        stdin: InputStream,
    ): String =
        if (rawPath == "-") {
            String(stdin.readBytes(), StandardCharsets.UTF_8)
        } else {
            resolveRepoRelative(rawPath).readText(StandardCharsets.UTF_8)
        }

    private fun resolveRepoRelative(rawPath: String): Path {
        val path = Path.of(rawPath)
        require(!path.isAbsolute) { "Use repo-relative paths only: $rawPath" }
        return projectRoot().resolve(path).normalize()
    }

    private fun renderMarking(marking: Map<String, List<dev.meshwork.model.Token>>): String {
        val root = JsonSupport.jsonMapper.createObjectNode()
        marking.toSortedMap().forEach { (placeId, tokens) ->
            val array = root.putArray(placeId)
            tokens.forEach { array.add(JsonSupport.cloneNode(it.value)) }
        }
        return JsonSupport.jsonMapper.writerWithDefaultPrettyPrinter().writeValueAsString(root)
    }

    private fun projectRoot(): Path =
        System.getProperty("meshwork.projectRoot")
            ?.let(Path::of)
            ?.toAbsolutePath()
            ?.normalize()
            ?: Path.of("").toAbsolutePath().normalize()
}
