// Package report demonstrates a reusable execution observer.
package report

import (
	"context"
	"fmt"
	"io"

	"example.com/meshwork-prototype/linker"
	"example.com/meshwork-prototype/plugins/api"
)

type Observer struct{ Output io.Writer }

func (Observer) Name() string                 { return "report" }
func (Observer) Enabled(linker.Scenario) bool { return true }
func (p Observer) Before(_ context.Context, request api.ExecutionRequest) error {
	_, err := fmt.Fprintf(p.Output, "starting: %s\n", request.Scenario.Name)
	return err
}
func (p Observer) After(_ context.Context, request api.ExecutionRequest, err error) {
	fmt.Fprintf(p.Output, "finished: %s (error: %v)\n", request.Scenario.Name, err)
}
