package dev.meshwork.io

import com.fasterxml.jackson.core.type.TypeReference
import com.fasterxml.jackson.databind.JsonNode
import dev.meshwork.model.GraphDocument
import dev.meshwork.model.ScenarioDocument
import dev.meshwork.support.JsonSupport
import dev.meshwork.support.MeshworkException
import java.nio.file.Path
import kotlin.io.path.inputStream

private val initialMarkingType = object : TypeReference<Map<String, List<JsonNode>>>() {}

fun loadGraph(path: Path): GraphDocument =
    path.inputStream().use { JsonSupport.yamlMapper.readValue(it, GraphDocument::class.java) }

fun loadScenario(path: Path): ScenarioDocument =
    path.inputStream().use { JsonSupport.yamlMapper.readValue(it, ScenarioDocument::class.java) }

fun parseInitialMarking(content: String): Map<String, List<JsonNode>> {
    if (content.isBlank()) {
        return emptyMap()
    }
    return try {
        JsonSupport.yamlMapper.readValue(content, initialMarkingType)
    } catch (error: Exception) {
        throw MeshworkException("Initial marking override must be YAML or JSON with shape {placeId: [tokens...]}. ${error.message}")
    }
}
