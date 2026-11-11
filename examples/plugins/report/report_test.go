package report_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cakesmith/meshwork/examples/plugins/report"
	"github.com/cakesmith/meshwork/linker"
	"github.com/cakesmith/meshwork/plugins/api"
	"github.com/cakesmith/meshwork/plugins/streamexec"
	"github.com/cakesmith/meshwork/plugins/testexec"
)

type executor struct{ err error }

func (executor) Name() string                                          { return "custom" }
func (executor) Enabled(linker.Scenario) bool                          { return true }
func (p executor) Execute(context.Context, api.ExecutionRequest) error { return p.err }

func TestIndependentComposition(t *testing.T) {
	var output bytes.Buffer
	failure := errors.New("execution failed")
	request := api.ExecutionRequest{Scenario: linker.Scenario{Name: "example"}}
	err := api.ExecuteScenario(context.Background(), request, report.Observer{Output: &output}, executor{err: failure})
	if !errors.Is(err, failure) || !strings.Contains(output.String(), "starting: example\nfinished: example (error: execution failed)") {
		t.Fatalf("error %v, output %q", err, output.String())
	}
	if err := api.ExecuteScenario(context.Background(), request, executor{}, streamexec.Plugin{}); err == nil {
		t.Fatal("accepted overlapping executors")
	}
	if !(testexec.Plugin{}).Enabled(linker.Scenario{Steps: []linker.Step{{States: map[string]string{"/x": "present"}}}}) {
		t.Fatal("test executor did not select step assertion")
	}
}
