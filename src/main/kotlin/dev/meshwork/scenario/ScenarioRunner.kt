package dev.meshwork.scenario

import dev.meshwork.io.loadScenario
import dev.meshwork.model.ScenarioMode
import dev.meshwork.runtime.MeshworkRuntime
import dev.meshwork.support.JsonSupport
import dev.meshwork.support.ScenarioAssertionException
import java.nio.file.Path

class ScenarioRunner(
    private val runtime: MeshworkRuntime,
) {
    fun run(
        scenarioPath: Path,
        projectRoot: Path,
        traceEnabled: Boolean = false,
    ): String {
        val scenario = loadScenario(scenarioPath)
        val graphPath = resolveRepoRelative(projectRoot, scenario.graph)
        val validatedGraph = runtime.loadValidatedGraph(graphPath, projectRoot)
        val stepLimit =
            when (scenario.mode) {
                ScenarioMode.UNTIL_IDLE -> null
                ScenarioMode.STEPS -> scenario.steps ?: throw ScenarioAssertionException("Scenario '${scenarioPath.fileName}' requires 'steps' when mode=STEPS.")
            }
        val result =
            runtime.runValidatedGraph(
                graph = validatedGraph,
                stepLimit = stepLimit,
                initialOverride = scenario.inputs,
                traceEnabled = traceEnabled,
            )

        scenario.expect.forEach { (placeId, expectedTokens) ->
            val actualTokens = result.marking[placeId].orEmpty().map { it.value }
            val place = validatedGraph.placeById.getValue(placeId)
            val expectedTyped =
                expectedTokens.map { token ->
                    try {
                        JsonSupport.coerceNode(token, place.type)
                    } catch (error: IllegalArgumentException) {
                        throw ScenarioAssertionException(
                            "Scenario expectation for '$placeId' does not match type ${place.type}: ${error.message}",
                        )
                    }
                }
            if (actualTokens.size != expectedTyped.size || actualTokens.zip(expectedTyped).any { (actual, expected) -> actual != expected }) {
                throw ScenarioAssertionException(
                    "Scenario assertion failed for '$placeId'. Expected ${JsonSupport.jsonMapper.writeValueAsString(expectedTyped)} but found ${JsonSupport.jsonMapper.writeValueAsString(actualTokens)}.",
                )
            }
        }

        return "Scenario passed: ${scenarioPath.fileName} (steps=${result.stepsExecuted}, idle=${result.terminatedBecauseIdle})"
    }

    private fun resolveRepoRelative(projectRoot: Path, rawPath: String): Path {
        val path = Path.of(rawPath)
        if (path.isAbsolute) {
            throw ScenarioAssertionException("Scenario graph path must be repo-relative: $rawPath")
        }
        return projectRoot.resolve(path).normalize()
    }
}
