// Package testexec executes scenarios with assertions.
package testexec

import (
	"context"
	"example.com/meshwork-prototype/linker"
	"example.com/meshwork-prototype/plugins/api"
	"fmt"
)

type Plugin struct{}

func (Plugin) Name() string                   { return "test" }
func (Plugin) Enabled(s linker.Scenario) bool { return api.HasExpectations(s) }
func (Plugin) Execute(ctx context.Context, request api.ExecutionRequest) error {
	if _, err := linker.RunScenarioWithRuntime(ctx, request.Graph, request.Scenario, request.Instantiate); err != nil {
		return err
	}
	_, err := fmt.Fprintf(request.Output, "passed: %s\n", request.Scenario.Name)
	return err
}
