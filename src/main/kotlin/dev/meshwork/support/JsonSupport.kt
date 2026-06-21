package dev.meshwork.support

import com.fasterxml.jackson.databind.DeserializationFeature
import com.fasterxml.jackson.databind.JsonNode
import com.fasterxml.jackson.databind.MapperFeature
import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.databind.json.JsonMapper
import com.fasterxml.jackson.dataformat.yaml.YAMLMapper
import com.fasterxml.jackson.module.kotlin.kotlinModule
import dev.meshwork.model.Token
import dev.meshwork.model.ValueType

object JsonSupport {
    val yamlMapper: ObjectMapper =
        YAMLMapper.builder()
            .addModule(kotlinModule())
            .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_ENUMS)
            .enable(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES)
            .build()

    val jsonMapper: ObjectMapper =
        JsonMapper.builder()
            .addModule(kotlinModule())
            .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_ENUMS)
            .enable(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES)
            .build()

    fun cloneNode(node: JsonNode): JsonNode = node.deepCopy<JsonNode>()

    fun renderTokenForText(token: Token): String =
        when (token.type) {
            ValueType.STRING -> token.value.asText()
            ValueType.INT -> token.value.asLong().toString()
            ValueType.BOOL -> token.value.asBoolean().toString()
            ValueType.OBJECT -> jsonMapper.writeValueAsString(token.value)
        }

    fun coerceNode(node: JsonNode, type: ValueType): JsonNode =
        when (type) {
            ValueType.STRING ->
                requireNode(node.isTextual) { "Expected string value but found $node" }
                    .let { jsonMapper.nodeFactory.textNode(node.asText()) }
            ValueType.INT ->
                requireNode(node.isIntegralNumber) { "Expected integer value but found $node" }
                    .let { jsonMapper.nodeFactory.numberNode(node.longValue()) }
            ValueType.BOOL ->
                requireNode(node.isBoolean) { "Expected boolean value but found $node" }
                    .let { jsonMapper.nodeFactory.booleanNode(node.booleanValue()) }
            ValueType.OBJECT -> cloneNode(node)
        }

    fun parseTextValue(raw: String, type: ValueType): JsonNode =
        when (type) {
            ValueType.STRING -> jsonMapper.nodeFactory.textNode(raw)
            ValueType.INT -> jsonMapper.nodeFactory.numberNode(raw.trim().toLong())
            ValueType.BOOL -> jsonMapper.nodeFactory.booleanNode(raw.trim().toBooleanStrict())
            ValueType.OBJECT -> jsonMapper.readTree(raw)
        }

    private inline fun requireNode(condition: Boolean, lazyMessage: () -> String) {
        if (!condition) {
            throw IllegalArgumentException(lazyMessage())
        }
    }
}
