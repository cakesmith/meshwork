package dev.meshwork.validation

import dev.meshwork.model.ArcDefinition
import dev.meshwork.model.ContractMode
import dev.meshwork.model.ExecutionConfiguration
import dev.meshwork.model.ExecutorDefinition
import dev.meshwork.model.ExecutorKind
import dev.meshwork.model.GraphDocument
import dev.meshwork.model.InputBinding
import dev.meshwork.model.MetricsPluginKind
import dev.meshwork.model.OutputBinding
import dev.meshwork.model.PlaceDefinition
import dev.meshwork.model.PlaceProtocolKind
import dev.meshwork.model.PortDefinition
import dev.meshwork.model.TransitionBinding
import dev.meshwork.model.TransitionDefinition
import dev.meshwork.model.ValidatedGraph
import dev.meshwork.support.ValidationException
import java.nio.file.Path

class GraphValidator {
    fun validate(
        graph: GraphDocument,
        graphPath: Path,
        projectRoot: Path,
    ): ValidatedGraph {
        val issues = mutableListOf<String>()
        val placeById = graph.places.associateBy { it.id }
        val transitionById = graph.transitions.associateBy { it.id }
        val allIds = graph.places.map { it.id } + graph.transitions.map { it.id }

        duplicateIds(allIds).forEach { issues += "Duplicate place/transition id '$it'." }
        duplicateIds(graph.execution.executors.map { it.id }).forEach { issues += "Duplicate executor id '$it'." }
        graph.transitions.forEach { transition ->
            duplicateIds(transition.inputs.map { it.id }).forEach { issues += "Transition '${transition.id}' has duplicate input port '$it'." }
            duplicateIds(transition.outputs.map { it.id }).forEach { issues += "Transition '${transition.id}' has duplicate output port '$it'." }
        }
        validatePlaceProtocols(graph, issues)

        val executorById = graph.execution.executors.associateBy { it.id }
        validateExecution(graph.execution, projectRoot, issues)

        val transitionBindings =
            graph.transitions.map { transition ->
                validateTransition(
                    transition = transition,
                    placeById = placeById,
                    transitionById = transitionById,
                    arcs = graph.arcs,
                    executorById = executorById,
                    defaultExecutorId = graph.execution.defaultExecutor,
                    projectRoot = projectRoot,
                    issues = issues,
                )
            }

        graph.initialMarking.forEach { (placeId, tokens) ->
            val place = placeById[placeId]
            if (place == null) {
                issues += "Initial marking references unknown place '$placeId'."
            } else {
                tokens.forEachIndexed { index, token ->
                    runCatching { dev.meshwork.support.JsonSupport.coerceNode(token, place.type) }
                        .onFailure { issues += "Initial marking token $index for '$placeId' is invalid: ${it.message}" }
                }
            }
        }

        if (issues.isNotEmpty()) {
            throw ValidationException(issues)
        }

        return ValidatedGraph(
            graph = graph,
            projectRoot = projectRoot,
            graphPath = graphPath,
            placeById = placeById,
            transitionBindings = transitionBindings.filterNotNull(),
        )
    }

    private fun validateExecution(
        execution: ExecutionConfiguration,
        projectRoot: Path,
        issues: MutableList<String>,
    ) {
        if (execution.executors.isEmpty()) {
            issues += "At least one executor must be defined in execution.executors."
        }
        execution.executors.forEach { executor ->
            if (executor.extensions.isEmpty()) {
                issues += "Executor '${executor.id}' must declare at least one supported extension."
            }
            executor.extensions.forEach { extension ->
                if (!extension.startsWith(".")) {
                    issues += "Executor '${executor.id}' has invalid extension '$extension'; include the leading dot."
                }
            }
            validateExecutorWrapper(executor, issues)
        }
        execution.defaultExecutor?.let { defaultExecutor ->
            if (execution.executors.none { it.id == defaultExecutor }) {
                issues += "execution.defaultExecutor '$defaultExecutor' does not match any registered executor."
            }
        }
        validateMetrics(execution, projectRoot, issues)
    }

    private fun validateMetrics(
        execution: ExecutionConfiguration,
        projectRoot: Path,
        issues: MutableList<String>,
    ) {
        val prometheusPaths = mutableSetOf<String>()
        execution.metrics.plugins.forEachIndexed { index, plugin ->
            when (plugin.kind) {
                MetricsPluginKind.PROMETHEUS -> {
                    val path = plugin.path ?: "/metrics"
                    if (!path.startsWith("/")) {
                        issues += "Metrics plugin #${index + 1} kind=PROMETHEUS path must start with '/'."
                    }
                    if (!prometheusPaths.add(path)) {
                        issues += "Duplicate Prometheus metrics path '$path'."
                    }
                    if (plugin.output != null) {
                        issues += "Metrics plugin kind=PROMETHEUS does not use 'output'."
                    }
                }
                MetricsPluginKind.FLAMEGRAPH -> {
                    if (plugin.path != null) {
                        issues += "Metrics plugin kind=FLAMEGRAPH does not use 'path'."
                    }
                    plugin.output?.let { output ->
                        resolveRepoRelativePath(output, projectRoot, "Metrics flamegraph output", issues)
                    }
                }
            }
        }
    }

    private fun validateExecutorWrapper(
        executor: ExecutorDefinition,
        issues: MutableList<String>,
    ) {
        val wrapper = executor.wrapper
        if (executor.kind == ExecutorKind.PROCESS) {
            if (wrapper == null) {
                issues += "Executor '${executor.id}' kind=PROCESS requires a wrapper block."
                return
            }
            if (wrapper.command.isEmpty()) {
                issues += "Executor '${executor.id}' wrapper.command must not be empty."
            }
        }

        if (wrapper != null) {
            if (wrapper.command.any { it.isBlank() }) {
                issues += "Executor '${executor.id}' wrapper.command entries must not be blank."
            }
            if (wrapper.timeoutMs <= 0) {
                issues += "Executor '${executor.id}' wrapper.timeoutMs must be positive."
            }
        }
    }

    private fun validatePlaceProtocols(graph: GraphDocument, issues: MutableList<String>) {
        val placeIds = graph.places.map { it.id }.toSet()
        val httpPaths = mutableMapOf<String, String>()

        graph.places.forEach { place ->
            val protocol = place.protocol ?: return@forEach
            when (protocol.kind) {
                PlaceProtocolKind.HTTP -> {
                    val path = protocol.path ?: defaultHttpPath(place.id)
                    if (!path.startsWith("/")) {
                        issues += "Place '${place.id}' HTTP protocol path must start with '/'."
                    }
                    val priorPlace = httpPaths.putIfAbsent(path, place.id)
                    if (priorPlace != null) {
                        issues += "Places '$priorPlace' and '${place.id}' share HTTP path '$path'."
                    }
                    protocol.awaitOutputs.forEach { outputPlaceId ->
                        if (outputPlaceId !in placeIds) {
                            issues += "Place '${place.id}' protocol.awaitOutputs references unknown place '$outputPlaceId'."
                        }
                    }
                }
                else -> {
                    if (protocol.path != null) {
                        issues += "Place '${place.id}' protocol.path is only valid for HTTP places."
                    }
                    if (protocol.awaitOutputs.isNotEmpty()) {
                        issues += "Place '${place.id}' protocol.awaitOutputs is only valid for HTTP places."
                    }
                }
            }
        }
    }

    private fun validateTransition(
        transition: TransitionDefinition,
        placeById: Map<String, PlaceDefinition>,
        transitionById: Map<String, TransitionDefinition>,
        arcs: List<ArcDefinition>,
        executorById: Map<String, ExecutorDefinition>,
        defaultExecutorId: String?,
        projectRoot: Path,
        issues: MutableList<String>,
    ): TransitionBinding? {
        val executorId = transition.logic.executor ?: defaultExecutorId
        if (executorId == null) {
            issues += "Transition '${transition.id}' does not declare an executor and no execution.defaultExecutor is set."
            return null
        }

        val executor = executorById[executorId]
        if (executor == null) {
            issues += "Transition '${transition.id}' references unknown executor '$executorId'."
            return null
        }

        val resolvedLogicPath = resolveLogicPath(transition.logic.path, projectRoot, transition.id, issues)
        val extension = extensionOf(transition.logic.path)
        if (executor.extensions.none { it.equals(extension, ignoreCase = true) }) {
            issues += "Transition '${transition.id}' uses '$extension' but executor '$executorId' supports ${executor.extensions.joinToString()}."
        }

        val inputBindings = mutableListOf<InputBinding>()
        transition.inputs.forEach { port ->
            val matchingArcs = arcs.filter { it.to == transition.id && it.toPort == port.id }
            if (matchingArcs.size != 1) {
                issues += "Transition '${transition.id}' input port '${port.id}' must have exactly one incoming place arc."
            }
            val arc = matchingArcs.singleOrNull()
            if (arc != null) {
                val sourcePlace = placeById[arc.from]
                if (sourcePlace == null) {
                    if (transitionById.containsKey(arc.from)) {
                        issues += "Arc '${arc.from} -> ${arc.to}' points from a transition into input port '${port.id}', but inputs must come from places."
                    } else {
                        issues += "Transition '${transition.id}' input port '${port.id}' references unknown place '${arc.from}'."
                    }
                } else if (sourcePlace.type != port.type) {
                    issues += "Type mismatch for '${transition.id}.${port.id}': place '${sourcePlace.id}' is ${sourcePlace.type} but port is ${port.type}."
                } else {
                    inputBindings += InputBinding(port = port, sourcePlace = sourcePlace)
                }
            }
        }

        val outputBindings = mutableListOf<OutputBinding>()
        transition.outputs.forEach { port ->
            val matchingArcs = arcs.filter { it.from == transition.id && it.fromPort == port.id }
            if (matchingArcs.isEmpty()) {
                issues += "Transition '${transition.id}' output port '${port.id}' must connect to at least one place."
            }

            val destinations = mutableListOf<PlaceDefinition>()
            matchingArcs.forEach { arc ->
                val destinationPlace = placeById[arc.to]
                if (destinationPlace == null) {
                    if (transitionById.containsKey(arc.to)) {
                        issues += "Arc '${arc.from} -> ${arc.to}' points into a transition from output port '${port.id}', but outputs must target places."
                    } else {
                        issues += "Transition '${transition.id}' output port '${port.id}' references unknown place '${arc.to}'."
                    }
                } else if (destinationPlace.type != port.type) {
                    issues += "Type mismatch for '${transition.id}.${port.id}': port is ${port.type} but place '${destinationPlace.id}' is ${destinationPlace.type}."
                } else {
                    destinations += destinationPlace
                }
            }
            if (destinations.isNotEmpty()) {
                outputBindings += OutputBinding(port = port, destinationPlaces = destinations)
            }
        }

        arcs.filter { it.from == transition.id && it.fromPort != null }
            .forEach { arc ->
                if (transition.outputs.none { it.id == arc.fromPort }) {
                    issues += "Transition '${transition.id}' arc references unknown output port '${arc.fromPort}'."
                }
            }

        arcs.filter { it.to == transition.id && it.toPort != null }
            .forEach { arc ->
                if (transition.inputs.none { it.id == arc.toPort }) {
                    issues += "Transition '${transition.id}' arc references unknown input port '${arc.toPort}'."
                }
            }

        val contract =
            transition.logic.contract
                ?: executor.contract
                ?: if (executor.kind == ExecutorKind.PROCESS) ContractMode.JSON else ContractMode.TEXT
        return if (resolvedLogicPath != null) {
            TransitionBinding(
                definition = transition,
                executor = executor,
                contract = contract,
                logicPath = resolvedLogicPath,
                inputs = inputBindings,
                outputs = outputBindings,
            )
        } else {
            null
        }
    }

    private fun resolveLogicPath(
        rawPath: String,
        projectRoot: Path,
        transitionId: String,
        issues: MutableList<String>,
    ): Path? {
        return resolveRepoRelativePath(rawPath, projectRoot, "Transition '$transitionId' logic path", issues)
    }

    private fun resolveRepoRelativePath(
        rawPath: String,
        projectRoot: Path,
        label: String,
        issues: MutableList<String>,
    ): Path? {
        val candidate = Path.of(rawPath)
        if (candidate.isAbsolute) {
            issues += "$label '$rawPath' must be repo-relative."
            return null
        }

        val resolved = projectRoot.resolve(candidate).normalize()
        if (!resolved.startsWith(projectRoot.normalize())) {
            issues += "$label '$rawPath' escapes the project root."
            return null
        }
        return resolved
    }

    private fun extensionOf(path: String): String = "." + path.substringAfterLast('.', "")

    private fun defaultHttpPath(placeId: String): String = "/places/$placeId"

    private fun duplicateIds(values: List<String>): Set<String> =
        values.groupingBy { it }.eachCount().filterValues { it > 1 }.keys
}
