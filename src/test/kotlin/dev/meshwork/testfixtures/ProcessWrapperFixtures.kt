package dev.meshwork.testfixtures

import com.fasterxml.jackson.databind.JsonNode
import com.fasterxml.jackson.databind.node.ObjectNode
import dev.meshwork.support.JsonSupport
import java.util.concurrent.atomic.AtomicInteger

object CompiledEchoWorker {
    @JvmStatic
    fun main(args: Array<String>) {
        val request = JsonSupport.jsonMapper.readTree(readLine() ?: error("missing request"))
        val response = JsonSupport.jsonMapper.createObjectNode()
        response.put("id", request["id"].asText())
        val outputs = response.putObject("outputs")
        outputs.put("message", request["inputs"]["message"].asText().uppercase())
        println(JsonSupport.jsonMapper.writeValueAsString(response))
    }
}

object PersistentCountingWorker {
    private val counter = AtomicInteger(0)

    @JvmStatic
    fun main(args: Array<String>) {
        val handshake = args.firstOrNull()
        handshake?.let(::println)
        generateSequence(::readLine).forEach { line ->
            val request = JsonSupport.jsonMapper.readTree(line)
            val response = JsonSupport.jsonMapper.createObjectNode()
            response.put("id", request["id"].asText())
            val outputs = response.putObject("outputs")
            val message = request["inputs"]["message"].asText()
            outputs.put("message", "${counter.incrementAndGet()}:$message")
            println(JsonSupport.jsonMapper.writeValueAsString(response))
        }
    }
}

object WrongIdWorker {
    @JvmStatic
    fun main(args: Array<String>) {
        val request = JsonSupport.jsonMapper.readTree(readLine() ?: error("missing request"))
        val response = JsonSupport.jsonMapper.createObjectNode()
        response.put("id", "wrong-${request["id"].asText()}")
        val outputs = response.putObject("outputs")
        outputs.put("message", "never")
        println(JsonSupport.jsonMapper.writeValueAsString(response))
    }
}

object SleepingWorker {
    @JvmStatic
    fun main(args: Array<String>) {
        val sleepMs = args.firstOrNull()?.toLongOrNull() ?: 200L
        val request = JsonSupport.jsonMapper.readTree(readLine() ?: error("missing request"))
        Thread.sleep(sleepMs)
        val response = JsonSupport.jsonMapper.createObjectNode()
        response.put("id", request["id"].asText())
        val outputs = response.putObject("outputs")
        outputs.put("message", "awake")
        println(JsonSupport.jsonMapper.writeValueAsString(response))
    }
}
