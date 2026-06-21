package dev.meshwork.model

import com.fasterxml.jackson.annotation.JsonProperty
import com.fasterxml.jackson.databind.JsonNode

enum class ValueType {
    STRING,
    INT,
    BOOL,
    OBJECT,
}

enum class ContractMode {
    TEXT,
    JSON,
}

enum class ExecutorKind {
    @JsonProperty("process")
    PROCESS,
    @JsonProperty("kotlin-process")
    KOTLIN_PROCESS,
}

enum class WrapperMode {
    ONESHOT,
    PERSISTENT,
}

enum class MetricsPluginKind {
    @JsonProperty("prometheus")
    PROMETHEUS,
    @JsonProperty("flamegraph")
    FLAMEGRAPH,
}

enum class PlaceProtocolKind {
    STDIN,
    STDOUT,
    HTTP,
}

enum class ScenarioMode {
    UNTIL_IDLE,
    STEPS,
}

data class GraphDocument(
    val execution: ExecutionConfiguration = ExecutionConfiguration(),
    val places: List<PlaceDefinition> = emptyList(),
    val transitions: List<TransitionDefinition> = emptyList(),
    val arcs: List<ArcDefinition> = emptyList(),
    val initialMarking: Map<String, List<JsonNode>> = emptyMap(),
)

data class ExecutionConfiguration(
    val executors: List<ExecutorDefinition> = emptyList(),
    val defaultExecutor: String? = null,
    val metrics: MetricsConfiguration = MetricsConfiguration(),
)

data class MetricsConfiguration(
    val plugins: List<MetricsPluginDefinition> = emptyList(),
)

data class MetricsPluginDefinition(
    val kind: MetricsPluginKind,
    val path: String? = null,
    val output: String? = null,
)

data class ExecutorDefinition(
    val id: String,
    val kind: ExecutorKind,
    val extensions: List<String> = emptyList(),
    val contract: ContractMode? = null,
    val wrapper: ExecutorWrapperDefinition? = null,
)

data class ExecutorWrapperDefinition(
    val command: List<String> = emptyList(),
    val mode: WrapperMode = WrapperMode.ONESHOT,
    val timeoutMs: Long = 30000,
    val handshake: String? = null,
)

data class PlaceDefinition(
    val id: String,
    val type: ValueType,
    val protocol: PlaceProtocolDefinition? = null,
)

data class PlaceProtocolDefinition(
    val kind: PlaceProtocolKind,
    val path: String? = null,
    val awaitOutputs: List<String> = emptyList(),
)

data class PortDefinition(
    val id: String,
    val type: ValueType,
)

data class TransitionDefinition(
    val id: String,
    val inputs: List<PortDefinition> = emptyList(),
    val outputs: List<PortDefinition> = emptyList(),
    val logic: TransitionLogicDefinition,
)

data class TransitionLogicDefinition(
    val path: String,
    val executor: String? = null,
    val contract: ContractMode? = null,
)

data class ArcDefinition(
    val from: String,
    val to: String,
    val fromPort: String? = null,
    val toPort: String? = null,
)

data class ScenarioDocument(
    val graph: String,
    val inputs: Map<String, List<JsonNode>> = emptyMap(),
    val mode: ScenarioMode = ScenarioMode.UNTIL_IDLE,
    val steps: Int? = null,
    val expect: Map<String, List<JsonNode>> = emptyMap(),
)

data class Token(
    val type: ValueType,
    val value: JsonNode,
)

data class InputBinding(
    val port: PortDefinition,
    val sourcePlace: PlaceDefinition,
)

data class OutputBinding(
    val port: PortDefinition,
    val destinationPlaces: List<PlaceDefinition>,
)

data class TransitionBinding(
    val definition: TransitionDefinition,
    val executor: ExecutorDefinition,
    val contract: ContractMode,
    val logicPath: java.nio.file.Path,
    val inputs: List<InputBinding>,
    val outputs: List<OutputBinding>,
)

data class ValidatedGraph(
    val graph: GraphDocument,
    val projectRoot: java.nio.file.Path,
    val graphPath: java.nio.file.Path,
    val placeById: Map<String, PlaceDefinition>,
    val transitionBindings: List<TransitionBinding>,
)

data class SimulationResult(
    val stepsExecuted: Int,
    val terminatedBecauseIdle: Boolean,
    val marking: Map<String, List<Token>>,
    val trace: List<String>,
)

data class EffectivePlaceProtocol(
    val place: PlaceDefinition,
    val kind: PlaceProtocolKind,
    val path: String? = null,
    val awaitOutputs: List<String> = emptyList(),
)
