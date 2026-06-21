package dev.meshwork.executor

import com.fasterxml.jackson.databind.JsonNode
import dev.meshwork.model.WrapperMode
import dev.meshwork.support.JsonSupport
import dev.meshwork.support.MeshworkException
import java.nio.file.Files
import java.nio.file.Path
import javax.script.Invocable
import javax.script.ScriptEngineManager
import kotlin.system.exitProcess

fun main(args: Array<String>) {
    try {
        require(args.isNotEmpty()) { "Expected the Kotlin logic file path." }
        val scriptPath = Path.of(args[0])
        val mode = args.getOrNull(1)?.let(WrapperMode::valueOf) ?: WrapperMode.ONESHOT
        val invocable = loadKotlinScript(scriptPath)

        when (mode) {
            WrapperMode.ONESHOT -> readLine()?.let { println(handleRequest(invocable, it)) }
            WrapperMode.PERSISTENT -> generateSequence(::readLine).forEach { println(handleRequest(invocable, it)) }
        }
    } catch (error: Throwable) {
        System.err.println(error.message ?: error::class.simpleName.orEmpty())
        System.err.println(error.stackTraceToString())
        exitProcess(1)
    }
}

private fun loadKotlinScript(scriptPath: Path): Invocable {
    if (!Files.exists(scriptPath)) {
        throw MeshworkException("Logic file does not exist: $scriptPath")
    }

    val engine = ScriptEngineManager().getEngineByExtension("kts")
        ?: throw MeshworkException("No Kotlin .kts script engine is available on the runtime classpath.")

    Files.newBufferedReader(scriptPath).use(engine::eval)
    return engine as? Invocable ?: throw MeshworkException("Loaded Kotlin script engine is not invocable.")
}

private fun handleRequest(
    invocable: Invocable,
    rawRequest: String,
): String {
    val request = JsonSupport.jsonMapper.readTree(rawRequest)
    val requestId = request["id"]?.asText()
        ?: throw MeshworkException("Worker request must include an 'id'.")

    val scriptInput =
        when {
            request.has("inputs") -> {
                val root = JsonSupport.jsonMapper.createObjectNode()
                root.set<JsonNode>("inputs", JsonSupport.cloneNode(request["inputs"]))
                JsonSupport.jsonMapper.writeValueAsString(root)
            }
            request.has("text") -> request["text"].asText()
            else -> throw MeshworkException("Worker request must include either 'inputs' or 'text'.")
        }

    val result = invocable.invokeFunction("run", scriptInput) ?: ""
    val rendered = renderScriptResult(result)
    val response = JsonSupport.jsonMapper.createObjectNode()
    response.put("id", requestId)

    if (request.has("inputs")) {
        val root = JsonSupport.jsonMapper.readTree(rendered)
        val outputs = root["outputs"] ?: throw MeshworkException("JSON contract result must include an 'outputs' object.")
        response.set<JsonNode>("outputs", JsonSupport.cloneNode(outputs))
    } else {
        response.put("text", rendered)
    }
    return JsonSupport.jsonMapper.writeValueAsString(response)
}

private fun renderScriptResult(result: Any): String =
    when (result) {
        is String -> result
        is CharSequence -> result.toString()
        is Number, is Boolean -> result.toString()
        else -> JsonSupport.jsonMapper.writeValueAsString(result)
    }
