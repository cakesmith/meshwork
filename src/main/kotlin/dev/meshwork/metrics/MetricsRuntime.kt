package dev.meshwork.metrics

import dev.meshwork.model.MetricsConfiguration
import dev.meshwork.model.MetricsPluginDefinition
import dev.meshwork.model.MetricsPluginKind
import dev.meshwork.support.MeshworkException
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.nio.file.Path
import java.util.Collections
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong
import kotlin.io.path.invariantSeparatorsPathString

data class TransitionMetricEvent(
    val transitionId: String,
    val executorId: String,
    val logicPath: String,
    val durationNanos: Long,
    val succeeded: Boolean,
)

data class MetricsHttpContext(
    val path: String,
    val contentType: String,
    val renderBody: () -> String,
)

class MetricsRuntime(
    projectRoot: Path,
    configuration: MetricsConfiguration,
    flamegraphOutputOverride: Path? = null,
) : AutoCloseable {
    private val plugins =
        buildList {
            configuration.plugins.forEach { add(createPlugin(projectRoot, it)) }
            flamegraphOutputOverride?.let { add(FlamegraphMetricsPlugin(it.normalize())) }
        }

    fun record(event: TransitionMetricEvent) {
        plugins.forEach { it.record(event) }
    }

    fun httpContexts(): List<MetricsHttpContext> = plugins.flatMap { it.httpContexts() }

    override fun close() {
        plugins.forEach(MetricsPlugin::close)
    }
}

private interface MetricsPlugin : AutoCloseable {
    fun record(event: TransitionMetricEvent)

    fun httpContexts(): List<MetricsHttpContext> = emptyList()
}

private fun createPlugin(
    projectRoot: Path,
    definition: MetricsPluginDefinition,
): MetricsPlugin =
    when (definition.kind) {
        MetricsPluginKind.PROMETHEUS -> PrometheusMetricsPlugin(definition.path ?: "/metrics")
        MetricsPluginKind.FLAMEGRAPH -> {
            val relativeOutput = definition.output ?: "build/metrics/flamegraph.folded"
            val outputPath = resolveRepoRelativeOutput(projectRoot, relativeOutput)
            FlamegraphMetricsPlugin(outputPath)
        }
    }

private fun resolveRepoRelativeOutput(
    projectRoot: Path,
    rawPath: String,
): Path {
    val candidate = Path.of(rawPath)
    if (candidate.isAbsolute) {
        throw MeshworkException("Metrics output path must be repo-relative: $rawPath")
    }
    val resolved = projectRoot.resolve(candidate).normalize()
    if (!resolved.startsWith(projectRoot.normalize())) {
        throw MeshworkException("Metrics output path escapes the project root: $rawPath")
    }
    return resolved
}

private class PrometheusMetricsPlugin(
    private val scrapePath: String,
) : MetricsPlugin {
    private val stats = ConcurrentHashMap<MetricKey, MetricAccumulator>()

    override fun record(event: TransitionMetricEvent) {
        val key = MetricKey(event.transitionId, event.executorId, event.logicPath)
        stats.computeIfAbsent(key) { MetricAccumulator() }.record(event)
    }

    override fun httpContexts(): List<MetricsHttpContext> =
        listOf(
            MetricsHttpContext(
                path = scrapePath,
                contentType = "text/plain; version=0.0.4; charset=utf-8",
                renderBody = ::renderPrometheusText,
            ),
        )

    override fun close() = Unit

    private fun renderPrometheusText(): String {
        val snapshots =
            stats.entries
                .map { (key, accumulator) -> key to accumulator.snapshot() }
                .sortedWith(compareBy({ it.first.transitionId }, { it.first.executorId }, { it.first.logicPath }))

        return buildString {
            appendLine("# HELP meshwork_transition_duration_seconds Total transition execution time.")
            appendLine("# TYPE meshwork_transition_duration_seconds summary")
            appendLine("# HELP meshwork_transition_failures_total Total failed transition executions.")
            appendLine("# TYPE meshwork_transition_failures_total counter")
            snapshots.forEach { (key, snapshot) ->
                val labels = prometheusLabels(key)
                appendLine("meshwork_transition_duration_seconds_count$labels ${snapshot.count}")
                appendLine("meshwork_transition_duration_seconds_sum$labels ${snapshot.totalNanos / 1_000_000_000.0}")
                appendLine("meshwork_transition_duration_seconds_max$labels ${snapshot.maxNanos / 1_000_000_000.0}")
                appendLine("meshwork_transition_failures_total$labels ${snapshot.failures}")
            }
        }
    }

    private fun prometheusLabels(key: MetricKey): String =
        """{transition="${escapePrometheusLabel(key.transitionId)}",executor="${escapePrometheusLabel(key.executorId)}",logic="${escapePrometheusLabel(key.logicPath)}"}"""
}

private data class MetricKey(
    val transitionId: String,
    val executorId: String,
    val logicPath: String,
)

private data class MetricSnapshot(
    val count: Long,
    val totalNanos: Long,
    val maxNanos: Long,
    val failures: Long,
)

private class MetricAccumulator {
    private val count = AtomicLong(0)
    private val totalNanos = AtomicLong(0)
    private val maxNanos = AtomicLong(0)
    private val failures = AtomicLong(0)

    fun record(event: TransitionMetricEvent) {
        count.incrementAndGet()
        totalNanos.addAndGet(event.durationNanos)
        maxNanos.accumulateAndGet(event.durationNanos) { current, next -> maxOf(current, next) }
        if (!event.succeeded) {
            failures.incrementAndGet()
        }
    }

    fun snapshot(): MetricSnapshot =
        MetricSnapshot(
            count = count.get(),
            totalNanos = totalNanos.get(),
            maxNanos = maxNanos.get(),
            failures = failures.get(),
        )
}

private class FlamegraphMetricsPlugin(
    private val outputPath: Path,
) : MetricsPlugin {
    private val lines = Collections.synchronizedList(mutableListOf<String>())

    override fun record(event: TransitionMetricEvent) {
        val micros = (event.durationNanos / 1_000L).coerceAtLeast(1L)
        val status = if (event.succeeded) "ok" else "error"
        lines += "meshwork;${foldedName(event.transitionId)};$status $micros"
    }

    override fun close() {
        outputPath.parent?.let(Files::createDirectories)
        val content = lines.joinToString(System.lineSeparator())
        Files.writeString(outputPath, content, StandardCharsets.UTF_8)
    }
}

private fun escapePrometheusLabel(value: String): String =
    value
        .replace("\\", "\\\\")
        .replace("\"", "\\\"")
        .replace("\n", "\\n")

private fun foldedName(value: String): String =
    value.map { character ->
        when (character) {
            ';', ' ' -> '_'
            else -> character
        }
    }.joinToString("")

fun transitionLogicMetricPath(
    projectRoot: Path,
    logicPath: Path,
): String =
    projectRoot.relativize(logicPath.normalize()).invariantSeparatorsPathString
