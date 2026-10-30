package plugins

import (
	"context"
	"testing"

	"example.com/meshwork-prototype/linker"
	"example.com/meshwork-prototype/meshwork"
)

type recordedExecutor struct{ calls *[]string }

func (p recordedExecutor) Name() string                 { return "executor" }
func (p recordedExecutor) Enabled(linker.Scenario) bool { return true }
func (p recordedExecutor) Execute(context.Context, ExecutionRequest) error {
	*p.calls = append(*p.calls, "execute")
	return nil
}

type recordedObserver struct{ calls *[]string }

func (p recordedObserver) Name() string                 { return "observer" }
func (p recordedObserver) Enabled(linker.Scenario) bool { return true }
func (p recordedObserver) Before(context.Context, ExecutionRequest) error {
	*p.calls = append(*p.calls, "before")
	return nil
}
func (p recordedObserver) After(_ context.Context, _ ExecutionRequest, err error) {
	if err != nil {
		*p.calls = append(*p.calls, "after-error")
		return
	}
	*p.calls = append(*p.calls, "after")
}

func TestExecutionPluginsComposeObserversAroundOneExecutor(t *testing.T) {
	var calls []string
	err := ExecuteScenario(context.Background(), ExecutionRequest{Scenario: linker.Scenario{Name: "test"}}, recordedObserver{&calls}, recordedExecutor{&calls})
	if err != nil || len(calls) != 3 || calls[0] != "before" || calls[1] != "execute" || calls[2] != "after" {
		t.Fatalf("calls %v, error %v", calls, err)
	}
	if err := ExecuteScenario(context.Background(), ExecutionRequest{}, recordedExecutor{&calls}, recordedExecutor{&calls}); err == nil {
		t.Fatal("accepted multiple executors")
	}
}

func TestBuiltInExecutionPluginSelection(t *testing.T) {
	want, _ := meshwork.NewToken("Bool", true)
	if !(TestExecutionPlugin{}).Enabled(linker.Scenario{Expect: map[string]meshwork.Token{"/Q": want}}) {
		t.Fatal("test plugin did not enable for expectation")
	}
	if !(StreamExecutionPlugin{}).Enabled(linker.Scenario{}) {
		t.Fatal("stream plugin did not enable without expectation")
	}
}
