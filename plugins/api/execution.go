// Package api defines scenario plugin contracts and their execution host.
package api

import (
	"context"
	"example.com/meshwork-prototype/linker"
	"example.com/meshwork-prototype/meshwork"
	"fmt"
	"io"
)

type ExecutionRequest struct {
	Graph           meshwork.ExecutableGraph
	Scenario        linker.Scenario
	Implementations map[string]meshwork.Compute
	NewRuntime      func(meshwork.Clock) (*meshwork.Runtime, error)
	Input           io.Reader
	Output          io.Writer
}

type ScenarioPlugin interface {
	Name() string
	Enabled(linker.Scenario) bool
}

type ExecutorPlugin interface {
	ScenarioPlugin
	Execute(context.Context, ExecutionRequest) error
}

// ExecutionObserver composes around the selected executor, for metrics and similar concerns.
type ExecutionObserver interface {
	ScenarioPlugin
	Before(context.Context, ExecutionRequest) error
	After(context.Context, ExecutionRequest, error)
}

// ExecuteScenario runs the enabled executor and all enabled lifecycle observers.
func ExecuteScenario(ctx context.Context, request ExecutionRequest, plugins ...ScenarioPlugin) (err error) {
	var executor ExecutorPlugin
	var observers []ExecutionObserver
	for _, plugin := range plugins {
		if !plugin.Enabled(request.Scenario) {
			continue
		}
		handled := false
		if next, ok := plugin.(ExecutorPlugin); ok {
			handled = true
			if executor != nil {
				return fmt.Errorf("multiple execution plugins enabled: %s and %s", executor.Name(), next.Name())
			}
			executor = next
		}
		if observer, ok := plugin.(ExecutionObserver); ok {
			handled = true
			observers = append(observers, observer)
		}
		if !handled {
			return fmt.Errorf("execution plugin %s has no executor or observer capability", plugin.Name())
		}
	}
	if executor == nil {
		return fmt.Errorf("no execution plugin enabled for scenario %s", request.Scenario.Name)
	}
	started := 0
	defer func() {
		for i := started - 1; i >= 0; i-- {
			observers[i].After(ctx, request, err)
		}
	}()
	for _, observer := range observers {
		if err = observer.Before(ctx, request); err != nil {
			return err
		}
		started++
	}
	return executor.Execute(ctx, request)
}

// HasExpectations reports whether a scenario has finite test assertions.
func HasExpectations(s linker.Scenario) bool {
	if len(s.Expect) != 0 {
		return true
	}
	for _, step := range s.Steps {
		if len(step.Expect) != 0 || len(step.States) != 0 {
			return true
		}
	}
	return false
}

// Instantiate uses explicitly bound executors when supplied by the host.
func (r ExecutionRequest) Instantiate(clock meshwork.Clock) (*meshwork.Runtime, error) {
	if r.NewRuntime != nil {
		return r.NewRuntime(clock)
	}
	return r.Graph.InstantiateWithClock(r.Implementations, clock)
}
