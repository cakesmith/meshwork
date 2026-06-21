import com.fasterxml.jackson.module.kotlin.jacksonObjectMapper

fun run(input: String): String {
    val mapper = jacksonObjectMapper()
    val root = mapper.readTree(input)
    val value = root["inputs"]["message"].asText()
    return mapper.writeValueAsString(
        mapOf(
            "outputs" to mapOf(
                "message" to "processed:$value",
            ),
        ),
    )
}

