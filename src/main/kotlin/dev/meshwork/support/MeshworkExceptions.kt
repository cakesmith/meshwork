package dev.meshwork.support

open class MeshworkException(message: String) : RuntimeException(message)

class ValidationException(
    val issues: List<String>,
) : MeshworkException(issues.joinToString(separator = System.lineSeparator()))

class ScenarioAssertionException(message: String) : MeshworkException(message)

