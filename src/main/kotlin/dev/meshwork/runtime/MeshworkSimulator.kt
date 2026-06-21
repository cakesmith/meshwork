package dev.meshwork.runtime

import com.fasterxml.jackson.databind.JsonNode
import com.fasterxml.jackson.databind.node.ObjectNode
import dev.meshwork.executor.ExecutorRuntime
import dev.meshwork.metrics.MetricsRuntime
import dev.meshwork.metrics.TransitionMetricEvent
import dev.meshwork.metrics.transitionLogicMetricPath
import dev.meshwork.model.ContractMode
import dev.meshwork.model.OutputBinding
import dev.meshwork.model.PlaceDefinition
import dev.meshwork.model.SimulationResult
import dev.meshwork.model.Token
import dev.meshwork.model.TransitionBinding
import dev.meshwork.model.ValidatedGraph
import dev.meshwork.support.JsonSupport
import dev.meshwork.support.MeshworkException
import java.util.Locale

class MeshworkSimulator {
    fun run(
        graph: ValidatedGraph,
        executors: ExecutorRuntime,
        metrics: MetricsRuntime,
        stepLimit: Int? = null,
        initialOverride: Map<String, List<com.fasterxml.jackson.databind.JsonNode>> = emptyMap(),
        traceEnabled: Boolean = false,
    ): SimulationResult =
        runOnMarking(
            graph = graph,
            executors = executors,
            metrics = metrics,
            marking = createInitialMarking(graph, initialOverride),
            stepLimit = stepLimit,
            traceEnabled = traceEnabled,
        )

    fun createInitialMarking(
        graph: ValidatedGraph,
        initialOverride: Map<String, List<com.fasterxml.jackson.databind.JsonNode>> = emptyMap(),
    ): MutableMap<String, ArrayDeque<Token>> {
        val marking = mutableMapOf<String, ArrayDeque<Token>>()
        graph.placeById.keys.forEach { marking[it] = ArrayDeque() }

        graph.graph.initialMarking.forEach { (placeId, tokens) ->
            val place = graph.placeById.getValue(placeId)
            tokens.map { Token(place.type, JsonSupport.coerceNode(it, place.type)) }
                .forEach { marking.getValue(placeId).addLast(it) }
        }

        initialOverride.forEach { (placeId, tokens) ->
            val place = graph.placeById[placeId] ?: throw MeshworkException("Scenario override references unknown place '$placeId'.")
            marking.getValue(placeId).clear()
            tokens.map { Token(place.type, JsonSupport.coerceNode(it, place.type)) }
                .forEach { marking.getValue(placeId).addLast(it) }
        }
        return marking
    }

    fun appendTokens(
        graph: ValidatedGraph,
        marking: MutableMap<String, ArrayDeque<Token>>,
        additions: Map<String, List<com.fasterxml.jackson.databind.JsonNode>>,
    ) {
        additions.forEach { (placeId, tokens) ->
            val place = graph.placeById[placeId] ?: throw MeshworkException("Server input references unknown place '$placeId'.")
            tokens.map { Token(place.type, JsonSupport.coerceNode(it, place.type)) }
                .forEach { marking.getValue(placeId).addLast(it) }
        }
    }

    fun drainTokens(
        marking: MutableMap<String, ArrayDeque<Token>>,
        placeId: String,
    ): List<Token> {
        val queue = marking.getValue(placeId)
        val drained = queue.toList()
        queue.clear()
        return drained
    }

    fun runOnMarking(
        graph: ValidatedGraph,
        executors: ExecutorRuntime,
        metrics: MetricsRuntime,
        marking: MutableMap<String, ArrayDeque<Token>>,
        stepLimit: Int? = null,
        traceEnabled: Boolean = false,
    ): SimulationResult {
        val trace = mutableListOf<String>()
        var stepsExecuted = 0

        while (stepLimit == null || stepsExecuted < stepLimit) {
            val next = graph.transitionBindings.firstOrNull { isEnabled(it, marking) } ?: break
            val transitionTrace = if (traceEnabled) mutableListOf<String>() else null
            executeTransition(graph, next, executors, metrics, marking, transitionTrace)
            stepsExecuted += 1
            if (transitionTrace != null) {
                trace += "step=$stepsExecuted transition=${next.definition.id} ${transitionTrace.joinToString(" | ")}"
            }
        }

        return SimulationResult(
            stepsExecuted = stepsExecuted,
            terminatedBecauseIdle = graph.transitionBindings.none { isEnabled(it, marking) },
            marking = marking.mapValues { (_, queue) -> queue.toList() },
            trace = trace,
        )
    }

    private fun isEnabled(
        transition: TransitionBinding,
        marking: Map<String, ArrayDeque<Token>>,
    ): Boolean = transition.inputs.all { marking.getValue(it.sourcePlace.id).isNotEmpty() }

    private fun executeTransition(
        graph: ValidatedGraph,
        transition: TransitionBinding,
        executors: ExecutorRuntime,
        metrics: MetricsRuntime,
        marking: MutableMap<String, ArrayDeque<Token>>,
        trace: MutableList<String>?,
    ) {
        val startedAt = System.nanoTime()
        val consumed = transition.inputs.map { input ->
            val token = marking.getValue(input.sourcePlace.id).removeFirst()
            input.port.id to token
        }
        trace?.add("consumed=${consumed.joinToString { "${it.first}=${JsonSupport.renderTokenForText(it.second)}" }}")

        try {
            val inputPayload = encodeInput(transition, consumed)
            val (stdout, stderr) = executors.execute(transition, inputPayload)
            if (stderr.isNotBlank()) {
                trace?.add("stderr=${stderr.trim()}")
            }

            val produced = decodeOutput(transition, stdout)
            val producedTrace = mutableListOf<String>()
            transition.outputs.forEach { binding ->
                val rawNode = produced.getValue(binding.port.id)
                routeOutputNodes(binding, rawNode, transition.definition.id).forEach { (place, node) ->
                    val typedNode = JsonSupport.coerceNode(node, place.type)
                    val token = Token(place.type, typedNode)
                    marking.getValue(place.id).addLast(Token(place.type, JsonSupport.cloneNode(token.value)))
                    producedTrace += "${place.id}=${JsonSupport.renderTokenForText(token)}"
                }
            }
            val durationNanos = System.nanoTime() - startedAt
            metrics.record(
                TransitionMetricEvent(
                    transitionId = transition.definition.id,
                    executorId = transition.executor.id,
                    logicPath = transitionLogicMetricPath(graph.projectRoot, transition.logicPath),
                    durationNanos = durationNanos,
                    succeeded = true,
                ),
            )
            trace?.add("durationMs=${"%.3f".format(Locale.ROOT, durationNanos / 1_000_000.0)}")
            trace?.add("produced=${producedTrace.joinToString()}")
        } catch (error: Exception) {
            metrics.record(
                TransitionMetricEvent(
                    transitionId = transition.definition.id,
                    executorId = transition.executor.id,
                    logicPath = transitionLogicMetricPath(graph.projectRoot, transition.logicPath),
                    durationNanos = System.nanoTime() - startedAt,
                    succeeded = false,
                ),
            )
            throw error
        }
    }

    private fun encodeInput(
        transition: TransitionBinding,
        consumed: List<Pair<String, Token>>,
    ): String =
        when (transition.contract) {
            ContractMode.TEXT -> consumed.joinToString(separator = "\n") { JsonSupport.renderTokenForText(it.second) }
            ContractMode.JSON -> {
                val root = JsonSupport.jsonMapper.createObjectNode()
                val inputsNode = root.putObject("inputs")
                consumed.forEach { (portId, token) -> inputsNode.set<JsonNode>(portId, JsonSupport.cloneNode(token.value)) }
                JsonSupport.jsonMapper.writeValueAsString(root)
            }
        }

    private fun decodeOutput(
        transition: TransitionBinding,
        stdout: String,
    ): Map<String, JsonNode> =
        when (transition.contract) {
            ContractMode.TEXT -> decodeTextOutput(transition, stdout)
            ContractMode.JSON -> decodeJsonOutput(transition, stdout)
        }

    private fun decodeTextOutput(
        transition: TransitionBinding,
        stdout: String,
    ): Map<String, JsonNode> {
        if (transition.outputs.isEmpty()) {
            return emptyMap()
        }

        val lines =
            if (transition.outputs.size == 1) {
                listOf(stdout.removeSuffix("\r").removeSuffix("\n"))
            } else {
                stdout.lines().filterNot { it == "" && stdout.endsWith(System.lineSeparator()) }
            }

        if (lines.size != transition.outputs.size) {
            throw MeshworkException(
                "Transition '${transition.definition.id}' returned ${lines.size} text outputs, expected ${transition.outputs.size}.",
            )
        }

        return transition.outputs.mapIndexed { index, binding ->
            binding.port.id to decodeTextOutputNode(lines[index], binding)
        }.toMap()
    }

    private fun decodeJsonOutput(
        transition: TransitionBinding,
        stdout: String,
    ): Map<String, JsonNode> {
        val root = JsonSupport.jsonMapper.readTree(stdout)
        val outputsNode = root["outputs"] ?: throw MeshworkException("JSON contract result must include an 'outputs' object.")
        if (outputsNode !is ObjectNode) {
            throw MeshworkException("JSON contract 'outputs' must be an object.")
        }

        return transition.outputs.associate { binding ->
            val rawNode = outputsNode[binding.port.id]
                ?: throw MeshworkException("JSON contract output '${binding.port.id}' was not returned by '${transition.definition.id}'.")
            binding.port.id to rawNode
        }
    }

    private fun decodeTextOutputNode(
        rawOutput: String,
        binding: OutputBinding,
    ): JsonNode = parseFanoutObject(rawOutput, binding) ?: JsonSupport.parseTextValue(rawOutput, binding.port.type)

    private fun routeOutputNodes(
        binding: OutputBinding,
        rawNode: JsonNode,
        transitionId: String,
    ): Map<PlaceDefinition, JsonNode> {
        val fanoutObject = rawNode as? ObjectNode ?: return duplicateOutput(binding, rawNode)
        if (binding.destinationPlaces.size < 2) {
            return duplicateOutput(binding, rawNode)
        }

        val destinationIds = binding.destinationPlaces.map { it.id }
        val matchedIds = destinationIds.filter(fanoutObject::has)
        if (matchedIds.isEmpty()) {
            return duplicateOutput(binding, rawNode)
        }
        if (matchedIds.size != destinationIds.size) {
            throw MeshworkException(
                "Transition '$transitionId' returned a partial fanout object for output '${binding.port.id}'. Expected keys for ${destinationIds.joinToString()}.",
            )
        }

        return binding.destinationPlaces.associateWith { place -> JsonSupport.cloneNode(fanoutObject.get(place.id)) }
    }

    private fun duplicateOutput(
        binding: OutputBinding,
        rawNode: JsonNode,
    ): Map<PlaceDefinition, JsonNode> =
        binding.destinationPlaces.associateWith { JsonSupport.cloneNode(rawNode) }

    private fun parseFanoutObject(
        rawOutput: String,
        binding: OutputBinding,
    ): JsonNode? {
        if (binding.destinationPlaces.size < 2) {
            return null
        }
        return runCatching { JsonSupport.jsonMapper.readTree(rawOutput) }
            .getOrNull()
            ?.takeIf { node ->
                node is ObjectNode && binding.destinationPlaces.any { destination -> node.has(destination.id) }
            }
    }
}
