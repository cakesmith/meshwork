// Package plugins provides scenario execution and the built-in plugins.
package plugins

import (
	"context"
	"example.com/meshwork-prototype/plugins/api"
	"example.com/meshwork-prototype/plugins/streamexec"
	"example.com/meshwork-prototype/plugins/testexec"
)

type ExecutionRequest = api.ExecutionRequest
type ScenarioPlugin = api.ScenarioPlugin
type ExecutorPlugin = api.ExecutorPlugin
type ExecutionObserver = api.ExecutionObserver
type TestExecutionPlugin = testexec.Plugin
type StreamExecutionPlugin = streamexec.Plugin

func ExecuteScenario(ctx context.Context, request ExecutionRequest, plugins ...ScenarioPlugin) error {
	return api.ExecuteScenario(ctx, request, plugins...)
}

func BuiltIns() []ScenarioPlugin {
	return []ScenarioPlugin{testexec.Plugin{}, streamexec.Plugin{}}
}
