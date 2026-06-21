package dev.meshwork.runtime

import com.fasterxml.jackson.databind.JsonNode
import dev.meshwork.executor.ExecutorRuntime
import dev.meshwork.io.loadGraph
import dev.meshwork.metrics.MetricsRuntime
import dev.meshwork.model.SimulationResult
import dev.meshwork.model.ValidatedGraph
import dev.meshwork.validation.GraphValidator
import java.nio.file.Path

class MeshworkRuntime(
    private val validator: GraphValidator = GraphValidator(),
    private val simulator: MeshworkSimulator = MeshworkSimulator(),
) {
    fun loadValidatedGraph(graphPath: Path, projectRoot: Path): ValidatedGraph {
        val graph = loadGraph(graphPath)
        return validator.validate(graph, graphPath, projectRoot.normalize())
    }

    fun runGraph(
        graphPath: Path,
        projectRoot: Path,
        stepLimit: Int? = null,
        initialOverride: Map<String, List<JsonNode>> = emptyMap(),
        traceEnabled: Boolean = false,
        flamegraphOutput: Path? = null,
    ): SimulationResult {
        val validated = loadValidatedGraph(graphPath, projectRoot)
        return runValidatedGraph(validated, stepLimit, initialOverride, traceEnabled, flamegraphOutput)
    }

    fun runValidatedGraph(
        graph: ValidatedGraph,
        stepLimit: Int? = null,
        initialOverride: Map<String, List<JsonNode>> = emptyMap(),
        traceEnabled: Boolean = false,
        flamegraphOutput: Path? = null,
    ): SimulationResult =
        ExecutorRuntime(graph.projectRoot).use { executors ->
            MetricsRuntime(graph.projectRoot, graph.graph.execution.metrics, flamegraphOutput).use { metrics ->
                simulator.run(
                    graph = graph,
                    executors = executors,
                    metrics = metrics,
                    stepLimit = stepLimit,
                    initialOverride = initialOverride,
                    traceEnabled = traceEnabled,
                )
            }
        }

    fun openServerSession(
        graphPath: Path,
        projectRoot: Path,
        options: ServerRuntimeOptions = ServerRuntimeOptions(),
        initialOverride: Map<String, List<JsonNode>> = emptyMap(),
        stdout: java.io.PrintStream = System.out,
        flamegraphOutput: Path? = null,
    ): MeshworkServerSession {
        val validated = loadValidatedGraph(graphPath, projectRoot)
        return openServerSession(validated, options, initialOverride, stdout, flamegraphOutput)
    }

    fun openServerSession(
        graph: ValidatedGraph,
        options: ServerRuntimeOptions = ServerRuntimeOptions(),
        initialOverride: Map<String, List<JsonNode>> = emptyMap(),
        stdout: java.io.PrintStream = System.out,
        flamegraphOutput: Path? = null,
    ): MeshworkServerSession {
        val protocols = resolveEffectivePlaceProtocols(graph, enableHttpOverlay = options.enableHttpOverlay)
        if (protocols.isEmpty()) {
            throw dev.meshwork.support.MeshworkException("Server mode requires at least one resolved stdin, stdout, or http place.")
        }
        return MeshworkServerSession(
            graph = graph,
            simulator = simulator,
            executors = ExecutorRuntime(graph.projectRoot),
            metrics = MetricsRuntime(graph.projectRoot, graph.graph.execution.metrics, flamegraphOutput),
            initialOverride = initialOverride,
            protocols = protocols,
            stdout = stdout,
            options = options,
        )
    }
}
