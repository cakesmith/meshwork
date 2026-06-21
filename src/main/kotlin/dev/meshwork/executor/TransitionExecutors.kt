package dev.meshwork.executor

import com.fasterxml.jackson.databind.JsonNode
import dev.meshwork.model.ContractMode
import dev.meshwork.model.ExecutorKind
import dev.meshwork.model.TransitionBinding
import dev.meshwork.model.WrapperMode
import dev.meshwork.support.JsonSupport
import dev.meshwork.support.MeshworkException
import java.nio.charset.StandardCharsets
import java.nio.file.Path
import java.util.UUID
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread
import kotlin.io.path.exists

class ExecutorRuntime(
    private val projectRoot: Path,
) : AutoCloseable {
    private val persistentSessions = linkedMapOf<SessionKey, ExecutorSession>()
    private val javaBinary = Path.of(System.getProperty("java.home"), "bin", javaBinaryName()).toString()
    private val classpath = System.getProperty("java.class.path")

    fun execute(
        transition: TransitionBinding,
        inputPayload: String,
    ): Pair<String, String> {
        val request = buildWorkerRequest(transition, inputPayload)
        val session = sessionFor(transition)
        val result = session.execute(transition, request)
        return result.stdoutPayload to result.stderr
    }

    override fun close() {
        persistentSessions.values.forEach {
            runCatching(it::close)
        }
        persistentSessions.clear()
    }

    private fun sessionFor(transition: TransitionBinding): ExecutorSession {
        val mode = transition.executor.wrapper?.mode ?: WrapperMode.ONESHOT
        if (mode == WrapperMode.ONESHOT) {
            return createSession(transition, mode)
        }

        val key = SessionKey(transition.executor.id, transition.logicPath, projectRoot)
        return persistentSessions.getOrPut(key) { createSession(transition, mode) }
    }

    private fun createSession(
        transition: TransitionBinding,
        mode: WrapperMode,
    ): ExecutorSession {
        val timeoutMs = transition.executor.wrapper?.timeoutMs ?: 30000L
        val handshake = transition.executor.wrapper?.handshake
        val command = buildCommand(transition, mode)
        return when (mode) {
            WrapperMode.ONESHOT -> OneShotProcessSession(projectRoot, command, timeoutMs)
            WrapperMode.PERSISTENT -> PersistentProcessSession(projectRoot, command, timeoutMs, handshake)
        }
    }

    private fun buildCommand(
        transition: TransitionBinding,
        mode: WrapperMode,
    ): List<String> {
        val baseCommand =
            when (transition.executor.kind) {
                ExecutorKind.PROCESS -> transition.executor.wrapper?.command.orEmpty()
                ExecutorKind.KOTLIN_PROCESS ->
                    listOf(
                        javaBinary,
                        "-cp",
                        classpath,
                        "dev.meshwork.executor.KotlinScriptWorkerKt",
                        transition.logicPath.toString(),
                        mode.name,
                    )
            }
        if (baseCommand.isEmpty()) {
            throw MeshworkException("Executor '${transition.executor.id}' has no launch command.")
        }

        return baseCommand.map { token ->
            val expanded =
                token
                    .replace("{logicPath}", transition.logicPath.toString())
                    .replace("{projectRoot}", projectRoot.toString())
                    .replace("{java}", javaBinary)
                    .replace("{classpath}", classpath)
            if (expanded.contains('{') || expanded.contains('}')) {
                throw MeshworkException("Executor '${transition.executor.id}' command token '$token' uses an unsupported placeholder.")
            }
            resolveRepoRelativeToken(expanded)
        }
    }

    private fun resolveRepoRelativeToken(token: String): String {
        val path = runCatching { Path.of(token) }.getOrNull() ?: return token
        if (path.isAbsolute) {
            return token
        }
        val resolved = projectRoot.resolve(path).normalize()
        return if (resolved.exists()) resolved.toString() else token
    }

    private fun buildWorkerRequest(
        transition: TransitionBinding,
        inputPayload: String,
    ): WorkerRequest =
        when (transition.contract) {
            ContractMode.JSON -> {
                val root = JsonSupport.jsonMapper.readTree(inputPayload)
                val inputs = root["inputs"] ?: throw MeshworkException("JSON contract payload must include an 'inputs' object.")
                WorkerRequest(id = UUID.randomUUID().toString(), inputs = JsonSupport.cloneNode(inputs))
            }
            ContractMode.TEXT -> WorkerRequest(id = UUID.randomUUID().toString(), text = inputPayload)
        }

    private fun javaBinaryName(): String = if (System.getProperty("os.name").startsWith("Windows")) "java.exe" else "java"
}

private data class SessionKey(
    val executorId: String,
    val logicPath: Path,
    val projectRoot: Path,
)

private data class WorkerRequest(
    val id: String,
    val inputs: JsonNode? = null,
    val text: String? = null,
)

private data class ExecutionResult(
    val stdoutPayload: String,
    val stderr: String,
)

private interface ExecutorSession : AutoCloseable {
    fun execute(
        transition: TransitionBinding,
        request: WorkerRequest,
    ): ExecutionResult
}

private class OneShotProcessSession(
    private val projectRoot: Path,
    private val command: List<String>,
    private val timeoutMs: Long,
) : ExecutorSession {
    override fun execute(
        transition: TransitionBinding,
        request: WorkerRequest,
    ): ExecutionResult {
        val process = ProcessBuilder(command).directory(projectRoot.toFile()).start()
        process.outputStream.bufferedWriter(StandardCharsets.UTF_8).use { writer ->
            writer.write(renderWorkerRequest(request))
            writer.newLine()
        }

        val stdoutBuffer = StringBuilder()
        val stderrBuffer = StringBuilder()
        val stdoutThread =
            thread(start = true) {
                process.inputStream.bufferedReader(StandardCharsets.UTF_8).use { stdoutBuffer.append(it.readText()) }
            }
        val stderrThread =
            thread(start = true) {
                process.errorStream.bufferedReader(StandardCharsets.UTF_8).use { stderrBuffer.append(it.readText()) }
            }

        if (!process.waitFor(timeoutMs, TimeUnit.MILLISECONDS)) {
            process.destroyForcibly()
            stdoutThread.join()
            stderrThread.join()
            throw MeshworkException("External executor timed out after ${timeoutMs}ms for ${transition.logicPath.fileName}.")
        }

        stdoutThread.join()
        stderrThread.join()
        if (process.exitValue() != 0) {
            throw MeshworkException(
                buildString {
                    append("External executor failed for ${transition.logicPath.fileName} with exit code ${process.exitValue()}.")
                    if (stderrBuffer.isNotBlank()) {
                        append(System.lineSeparator())
                        append(stderrBuffer.toString().trim())
                    }
                },
            )
        }

        val responseLine =
            stdoutBuffer
                .lineSequence()
                .firstOrNull { it.isNotBlank() }
                ?: throw MeshworkException("External executor produced no response for ${transition.logicPath.fileName}.")
        return ExecutionResult(
            stdoutPayload = parseWorkerResponse(responseLine, request.id, transition.contract),
            stderr = stderrBuffer.toString().trim(),
        )
    }

    override fun close() = Unit
}

private class PersistentProcessSession(
    private val projectRoot: Path,
    private val command: List<String>,
    private val timeoutMs: Long,
    private val handshake: String?,
) : ExecutorSession {
    private val stderrLock = Any()
    private var process: Process? = null
    private var writer: java.io.BufferedWriter? = null
    private var responseQueue: LinkedBlockingQueue<String>? = null
    private var stdoutThread: Thread? = null
    private var stderrThread: Thread? = null
    private val stderrBuffer = StringBuilder()

    override fun execute(
        transition: TransitionBinding,
        request: WorkerRequest,
    ): ExecutionResult {
        ensureStarted(transition)
        val stderrOffset = synchronized(stderrLock) { stderrBuffer.length }
        val queue = responseQueue ?: throw MeshworkException("Persistent executor session failed to initialize.")
        val currentWriter = writer ?: throw MeshworkException("Persistent executor writer is unavailable.")

        try {
            currentWriter.write(renderWorkerRequest(request))
            currentWriter.newLine()
            currentWriter.flush()
        } catch (error: Exception) {
            destroyProcess()
            throw MeshworkException("Failed to send request to external executor for ${transition.logicPath.fileName}: ${error.message}")
        }

        val responseLine = queue.poll(timeoutMs, TimeUnit.MILLISECONDS)
        if (responseLine == null) {
            val exited = process?.let { !it.isAlive } ?: true
            val message =
                if (exited) {
                    "Persistent executor exited before responding for ${transition.logicPath.fileName}."
                } else {
                    "Persistent executor timed out after ${timeoutMs}ms for ${transition.logicPath.fileName}."
                }
            destroyProcess()
            throw MeshworkException(message)
        }

        val stderr = synchronized(stderrLock) { stderrBuffer.substring(stderrOffset).trim() }
        return ExecutionResult(
            stdoutPayload = parseWorkerResponse(responseLine, request.id, transition.contract),
            stderr = stderr,
        )
    }

    override fun close() {
        destroyProcess()
    }

    private fun ensureStarted(transition: TransitionBinding) {
        if (process?.isAlive == true) {
            return
        }

        destroyProcess()
        val started = ProcessBuilder(command).directory(projectRoot.toFile()).start()
        process = started
        writer = started.outputStream.bufferedWriter(StandardCharsets.UTF_8)
        responseQueue = LinkedBlockingQueue()
        synchronized(stderrLock) {
            stderrBuffer.clear()
        }

        stdoutThread =
            thread(start = true, isDaemon = true) {
                started.inputStream.bufferedReader(StandardCharsets.UTF_8).useLines { lines ->
                    lines.forEach { line -> responseQueue?.put(line) }
                }
            }
        stderrThread =
            thread(start = true, isDaemon = true) {
                started.errorStream.bufferedReader(StandardCharsets.UTF_8).useLines { lines ->
                    lines.forEach { line ->
                        synchronized(stderrLock) {
                            if (stderrBuffer.isNotEmpty()) {
                                stderrBuffer.append(System.lineSeparator())
                            }
                            stderrBuffer.append(line)
                        }
                    }
                }
            }

        handshake?.let { expected ->
            val line = responseQueue?.poll(timeoutMs, TimeUnit.MILLISECONDS)
            if (line != expected) {
                destroyProcess()
                throw MeshworkException(
                    "Persistent executor handshake failed for ${transition.logicPath.fileName}. Expected '$expected' but received '${line ?: "<none>"}'.",
                )
            }
        }
    }

    private fun destroyProcess() {
        runCatching { writer?.close() }
        writer = null
        responseQueue = null
        process?.destroyForcibly()
        process = null
        stdoutThread = null
        stderrThread = null
    }
}

private fun renderWorkerRequest(request: WorkerRequest): String {
    val root = JsonSupport.jsonMapper.createObjectNode()
    root.put("id", request.id)
    request.inputs?.let { root.set<JsonNode>("inputs", JsonSupport.cloneNode(it)) }
    request.text?.let { root.put("text", it) }
    return JsonSupport.jsonMapper.writeValueAsString(root)
}

private fun parseWorkerResponse(
    line: String,
    expectedId: String,
    contract: ContractMode,
): String {
    val root = JsonSupport.jsonMapper.readTree(line)
    val actualId = root["id"]?.asText()
    if (actualId != expectedId) {
        throw MeshworkException("External executor response id mismatch. Expected '$expectedId' but received '${actualId ?: "<missing>"}'.")
    }

    return when (contract) {
        ContractMode.JSON -> {
            val outputs = root["outputs"] ?: throw MeshworkException("External executor JSON response must include an 'outputs' object.")
            val responseRoot = JsonSupport.jsonMapper.createObjectNode()
            responseRoot.set<JsonNode>("outputs", JsonSupport.cloneNode(outputs))
            JsonSupport.jsonMapper.writeValueAsString(responseRoot)
        }
        ContractMode.TEXT -> root["text"]?.asText()
            ?: throw MeshworkException("External executor text response must include a 'text' field.")
    }
}
