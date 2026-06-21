package dev.meshwork.runtime

import com.fasterxml.jackson.databind.JsonNode
import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import dev.meshwork.executor.ExecutorRuntime
import dev.meshwork.metrics.MetricsHttpContext
import dev.meshwork.metrics.MetricsRuntime
import dev.meshwork.model.EffectivePlaceProtocol
import dev.meshwork.model.PlaceDefinition
import dev.meshwork.model.PlaceProtocolDefinition
import dev.meshwork.model.PlaceProtocolKind
import dev.meshwork.model.Token
import dev.meshwork.model.ValidatedGraph
import dev.meshwork.support.JsonSupport
import dev.meshwork.support.MeshworkException
import java.io.InputStream
import java.io.PrintStream
import java.net.InetSocketAddress
import java.nio.charset.StandardCharsets
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

data class ServerRuntimeOptions(
    val enableHttpOverlay: Boolean = false,
    val httpHost: String = "127.0.0.1",
    val httpPort: Int = 8080,
    val timeoutMs: Long = 30000,
)

fun resolveEffectivePlaceProtocols(
    graph: ValidatedGraph,
    enableHttpOverlay: Boolean = false,
): Map<String, EffectivePlaceProtocol> {
    val incomingCounts = graph.placeById.keys.associateWith { 0 }.toMutableMap()
    val outgoingCounts = graph.placeById.keys.associateWith { 0 }.toMutableMap()

    graph.graph.arcs.forEach { arc ->
        if (arc.from in graph.placeById && arc.toPort != null) {
            outgoingCounts.computeIfPresent(arc.from) { _, count -> count + 1 }
        }
        if (arc.to in graph.placeById && arc.fromPort != null) {
            incomingCounts.computeIfPresent(arc.to) { _, count -> count + 1 }
        }
    }

    val resolved =
        graph.placeById.values.mapNotNull { place ->
            val explicit = place.protocol
            when {
                explicit != null -> explicitProtocol(place, explicit)
                enableHttpOverlay -> EffectivePlaceProtocol(place = place, kind = PlaceProtocolKind.HTTP, path = defaultHttpPath(place.id))
                incomingCounts.getValue(place.id) == 0 && outgoingCounts.getValue(place.id) > 0 ->
                    EffectivePlaceProtocol(place = place, kind = PlaceProtocolKind.STDIN)
                outgoingCounts.getValue(place.id) == 0 && incomingCounts.getValue(place.id) > 0 ->
                    EffectivePlaceProtocol(place = place, kind = PlaceProtocolKind.STDOUT)
                else -> null
            }
        }.associateBy { it.place.id }

    validateEffectiveProtocols(resolved)
    return resolved
}

class MeshworkServerSession(
    private val graph: ValidatedGraph,
    private val simulator: MeshworkSimulator,
    private val executors: ExecutorRuntime,
    private val metrics: MetricsRuntime,
    initialOverride: Map<String, List<JsonNode>> = emptyMap(),
    private val protocols: Map<String, EffectivePlaceProtocol>,
    private val stdout: PrintStream,
    private val options: ServerRuntimeOptions,
) : AutoCloseable {
    private val lock = ReentrantLock()
    private val tokenCondition = lock.newCondition()
    private val shutdownLatch = CountDownLatch(1)
    private val marking = simulator.createInitialMarking(graph, initialOverride)
    private val stdinProtocols = protocols.values.filter { it.kind == PlaceProtocolKind.STDIN }.sortedBy { it.place.id }
    private val stdoutProtocols = protocols.values.filter { it.kind == PlaceProtocolKind.STDOUT }.sortedBy { it.place.id }
    private val httpProtocols = protocols.values.filter { it.kind == PlaceProtocolKind.HTTP }.sortedBy { it.place.id }
    private var httpServer: HttpServer? = null
    private var closed = false

    val hasHttpServer: Boolean
        get() = httpProtocols.isNotEmpty() || metrics.httpContexts().isNotEmpty()

    val boundHttpPort: Int?
        get() = httpServer?.address?.port

    fun protocolSummary(): Map<String, EffectivePlaceProtocol> = protocols

    fun metricsPaths(): List<String> = metrics.httpContexts().map { it.path }.sorted()

    fun start() {
        val metricContexts = metrics.httpContexts()
        if (httpProtocols.isEmpty() && metricContexts.isEmpty()) {
            return
        }

        val server = HttpServer.create(InetSocketAddress(options.httpHost, options.httpPort), 0)
        httpProtocols.forEach { protocol ->
            val path = protocol.path ?: defaultHttpPath(protocol.place.id)
            server.createContext(path) { exchange -> handleHttpExchange(protocol, exchange) }
        }
        registerMetricsContexts(server, metricContexts)
        server.executor = Executors.newCachedThreadPool()
        server.start()
        httpServer = server
    }

    fun runStdinLoop(stdin: InputStream) {
        if (stdinProtocols.isEmpty()) {
            return
        }

        stdin.bufferedReader(StandardCharsets.UTF_8).useLines { lines ->
            lines.forEach { line ->
                if (line.isEmpty()) {
                    return@forEach
                }
                processStdinLine(line)
            }
        }
    }

    fun awaitShutdown() {
        shutdownLatch.await()
    }

    fun processStdinLine(line: String) {
        processEvent(
            inputs = parseStdinInputs(line),
            awaitOutputs = emptyList(),
        )
    }

    fun drainHttpPlace(placeId: String): Map<String, List<Token>> =
        lock.withLock {
            val protocol = protocols[placeId] ?: throw MeshworkException("Unknown protocol place '$placeId'.")
            require(protocol.kind == PlaceProtocolKind.HTTP) { "Place '$placeId' is not HTTP-bound." }
            mapOf(placeId to simulator.drainTokens(marking, placeId))
        }

    override fun close() {
        lock.withLock {
            if (closed) {
                return
            }
            closed = true
        }
        httpServer?.stop(0)
        executors.close()
        metrics.close()
        shutdownLatch.countDown()
    }

    private fun handleHttpExchange(
        protocol: EffectivePlaceProtocol,
        exchange: HttpExchange,
    ) {
        try {
            when (exchange.requestMethod.uppercase()) {
                "GET" -> writeJson(exchange, 200, renderTokenMap(drainHttpPlace(protocol.place.id)))
                "POST" -> handleHttpPost(protocol, exchange)
                else -> {
                    exchange.responseHeaders.add("Allow", "GET, POST")
                    writeJson(exchange, 405, """{"error":"Method not allowed"}""")
                }
            }
        } catch (error: IllegalArgumentException) {
            writeJson(exchange, 400, jsonError(error.message ?: "Bad request"))
        } catch (error: MeshworkException) {
            writeJson(exchange, 400, jsonError(error.message ?: "Bad request"))
        } catch (error: Exception) {
            writeJson(exchange, 500, jsonError(error.message ?: "Internal server error"))
        } finally {
            exchange.close()
        }
    }

    private fun handleHttpPost(
        protocol: EffectivePlaceProtocol,
        exchange: HttpExchange,
    ) {
        val tokens = parseHttpBody(protocol.place, exchange)
        val response =
            processEvent(
                inputs = mapOf(protocol.place.id to tokens),
                awaitOutputs = protocol.awaitOutputs,
            )

        when (response) {
            is HttpEventResponse.Accepted ->
                writeJson(
                    exchange,
                    202,
                    """{"status":"accepted","placeId":"${protocol.place.id}","received":${response.receivedCount}}""",
                )
            is HttpEventResponse.Completed -> writeJson(exchange, 200, renderTokenMap(response.outputs))
            is HttpEventResponse.Timeout ->
                writeJson(
                    exchange,
                    504,
                    buildString {
                        append("{")
                        append(""""received":${renderTokenMap(response.outputs)},""")
                        append(""""pending":${JsonSupport.jsonMapper.writeValueAsString(response.pendingPlaces)},""")
                        append(""""timeoutMs":${options.timeoutMs}""")
                        append("}")
                    },
                )
        }
    }

    private fun parseStdinInputs(line: String): Map<String, List<JsonNode>> =
        if (stdinProtocols.size == 1) {
            val protocol = stdinProtocols.single()
            mapOf(protocol.place.id to listOf(JsonSupport.parseTextValue(line, protocol.place.type)))
        } else {
            val root = JsonSupport.jsonMapper.readTree(line)
            require(root.isObject) { "Multi-place stdin expects one JSON object per line." }
            stdinProtocols.associateNotNull { protocol ->
                val node = root.get(protocol.place.id) ?: return@associateNotNull null
                require(node.isArray) { "stdin event place '${protocol.place.id}' must be an array of tokens." }
                protocol.place.id to node.toList().map(JsonSupport::cloneNode)
            }.also { inputs ->
                require(inputs.isNotEmpty()) { "stdin event must include at least one stdin-bound place." }
            }
        }

    private fun parseHttpBody(
        place: PlaceDefinition,
        exchange: HttpExchange,
    ): List<JsonNode> {
        val body = String(exchange.requestBody.readBytes(), StandardCharsets.UTF_8)
        val contentType = exchange.requestHeaders.getFirst("Content-Type").orEmpty()
        if (contentType.substringBefore(";").equals("application/json", ignoreCase = true)) {
            val root = JsonSupport.jsonMapper.readTree(body)
            return if (root.isArray) {
                root.toList().map(JsonSupport::cloneNode)
            } else {
                listOf(JsonSupport.cloneNode(root))
            }
        }
        return listOf(JsonSupport.parseTextValue(body, place.type))
    }

    private fun processEvent(
        inputs: Map<String, List<JsonNode>>,
        awaitOutputs: List<String>,
    ): HttpEventResponse =
        lock.withLock {
            val baselines = awaitOutputs.associateWith { marking.getValue(it).size }
            simulator.appendTokens(graph, marking, inputs)
            simulator.runOnMarking(graph, executors, metrics, marking)
            emitStdoutLocked()
            tokenCondition.signalAll()
            if (awaitOutputs.isEmpty()) {
                return HttpEventResponse.Accepted(inputs.values.sumOf { it.size })
            }

            val deadlineNanos = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(options.timeoutMs)
            while (awaitOutputs.any { marking.getValue(it).size <= baselines.getValue(it) }) {
                val remaining = deadlineNanos - System.nanoTime()
                if (remaining <= 0) {
                    val outputs = drainNewTokensLocked(awaitOutputs, baselines)
                    val pending = awaitOutputs.filter { outputs[it].isNullOrEmpty() }
                    return HttpEventResponse.Timeout(outputs, pending)
                }
                tokenCondition.awaitNanos(remaining)
            }
            HttpEventResponse.Completed(drainNewTokensLocked(awaitOutputs, baselines))
        }

    private fun emitStdoutLocked() {
        if (stdoutProtocols.isEmpty()) {
            return
        }

        val drained =
            stdoutProtocols.associate { it.place.id to simulator.drainTokens(marking, it.place.id) }
                .filterValues { it.isNotEmpty() }
        if (drained.isEmpty()) {
            return
        }

        if (stdoutProtocols.size == 1) {
            drained.values.single().forEach { token -> stdout.println(JsonSupport.renderTokenForText(token)) }
        } else {
            stdout.println(renderTokenMap(drained))
        }
        stdout.flush()
    }

    private fun drainNewTokensLocked(
        placeIds: List<String>,
        baselines: Map<String, Int>,
    ): Map<String, List<Token>> =
        placeIds.associateWith { placeId ->
            val queue = marking.getValue(placeId)
            val current = queue.toList()
            val keepCount = baselines.getValue(placeId).coerceAtMost(current.size)
            val kept = current.take(keepCount)
            val drained = current.drop(keepCount)
            queue.clear()
            kept.forEach(queue::addLast)
            drained
        }

    private fun renderTokenMap(outputs: Map<String, List<Token>>): String {
        val root = JsonSupport.jsonMapper.createObjectNode()
        outputs.toSortedMap().forEach { (placeId, tokens) ->
            val array = root.putArray(placeId)
            tokens.forEach { array.add(JsonSupport.cloneNode(it.value)) }
        }
        return JsonSupport.jsonMapper.writeValueAsString(root)
    }

    private fun jsonError(message: String): String = JsonSupport.jsonMapper.writeValueAsString(mapOf("error" to message))

    private sealed interface HttpEventResponse {
        data class Accepted(
            val receivedCount: Int,
        ) : HttpEventResponse

        data class Completed(
            val outputs: Map<String, List<Token>>,
        ) : HttpEventResponse

        data class Timeout(
            val outputs: Map<String, List<Token>>,
            val pendingPlaces: List<String>,
        ) : HttpEventResponse
    }

    private fun registerMetricsContexts(
        server: HttpServer,
        contexts: List<MetricsHttpContext>,
    ) {
        val placePaths = httpProtocols.map { it.path ?: defaultHttpPath(it.place.id) }.toMutableSet()
        contexts.forEach { context ->
            if (!placePaths.add(context.path)) {
                throw MeshworkException("Metrics path '${context.path}' conflicts with an existing HTTP place path.")
            }
            server.createContext(context.path) { exchange -> handleMetricsExchange(context, exchange) }
        }
    }

    private fun handleMetricsExchange(
        context: MetricsHttpContext,
        exchange: HttpExchange,
    ) {
        try {
            if (exchange.requestMethod.uppercase() != "GET") {
                exchange.responseHeaders.add("Allow", "GET")
                writeText(exchange, 405, "Method not allowed", "text/plain; charset=utf-8")
                return
            }
            writeText(exchange, 200, context.renderBody(), context.contentType)
        } finally {
            exchange.close()
        }
    }
}

private fun explicitProtocol(
    place: PlaceDefinition,
    protocol: PlaceProtocolDefinition,
): EffectivePlaceProtocol =
    EffectivePlaceProtocol(
        place = place,
        kind = protocol.kind,
        path = if (protocol.kind == PlaceProtocolKind.HTTP) protocol.path ?: defaultHttpPath(place.id) else null,
        awaitOutputs = if (protocol.kind == PlaceProtocolKind.HTTP) protocol.awaitOutputs else emptyList(),
    )

private fun validateEffectiveProtocols(protocols: Map<String, EffectivePlaceProtocol>) {
    val httpPaths = mutableMapOf<String, String>()
    protocols.values
        .filter { it.kind == PlaceProtocolKind.HTTP }
        .forEach { protocol ->
            val path = protocol.path ?: defaultHttpPath(protocol.place.id)
            val priorPlace = httpPaths.putIfAbsent(path, protocol.place.id)
            if (priorPlace != null) {
                throw MeshworkException("Places '$priorPlace' and '${protocol.place.id}' share HTTP path '$path'.")
            }
        }

    protocols.values
        .filter { it.kind == PlaceProtocolKind.HTTP }
        .forEach { protocol ->
            protocol.awaitOutputs.forEach { outputPlaceId ->
                val outputProtocol = protocols[outputPlaceId]
                if (outputProtocol?.kind != PlaceProtocolKind.HTTP) {
                    throw MeshworkException(
                        "Place '${protocol.place.id}' awaits '$outputPlaceId', but awaited outputs must resolve to HTTP places in server mode.",
                    )
                }
            }
        }
}

private fun defaultHttpPath(placeId: String): String = "/places/$placeId"

private inline fun <T, R> Iterable<T>.associateNotNull(transform: (T) -> Pair<String, R>?): Map<String, R> {
    val result = linkedMapOf<String, R>()
    for (item in this) {
        val pair = transform(item) ?: continue
        result[pair.first] = pair.second
    }
    return result
}

private fun writeJson(
    exchange: HttpExchange,
    statusCode: Int,
    body: String,
) {
    writeText(exchange, statusCode, body, "application/json; charset=utf-8")
}

private fun writeText(
    exchange: HttpExchange,
    statusCode: Int,
    body: String,
    contentType: String,
) {
    val bytes = body.toByteArray(StandardCharsets.UTF_8)
    exchange.responseHeaders.set("Content-Type", contentType)
    exchange.sendResponseHeaders(statusCode, bytes.size.toLong())
    exchange.responseBody.use { it.write(bytes) }
}
