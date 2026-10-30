// Package plugins contains optional execution adapters over linked Meshwork scenarios.
package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"example.com/meshwork-prototype/linker"
	"example.com/meshwork-prototype/meshwork"
)

type ExecutionRequest struct {
	Graph           meshwork.ExecutableGraph
	Scenario        linker.Scenario
	Implementations map[string]meshwork.Compute
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

func BuiltIns() []ScenarioPlugin {
	return []ScenarioPlugin{TestExecutionPlugin{}, StreamExecutionPlugin{}}
}

type TestExecutionPlugin struct{}

func (TestExecutionPlugin) Name() string { return "test" }
func (TestExecutionPlugin) Enabled(s linker.Scenario) bool {
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
func (TestExecutionPlugin) Execute(ctx context.Context, request ExecutionRequest) error {
	if _, err := linker.RunScenario(ctx, request.Graph, request.Scenario, request.Implementations); err != nil {
		return err
	}
	_, err := fmt.Fprintf(request.Output, "passed: %s\n", request.Scenario.Name)
	return err
}

type StreamExecutionPlugin struct{}

func (StreamExecutionPlugin) Name() string { return "stream" }
func (StreamExecutionPlugin) Enabled(s linker.Scenario) bool {
	return !(TestExecutionPlugin{}).Enabled(s)
}
func (StreamExecutionPlugin) Execute(ctx context.Context, request ExecutionRequest) error {
	s := request.Scenario
	if s.Input != "stdin" || s.Output != "stdout" {
		return fmt.Errorf("scenario %s is not a streaming scenario", s.Name)
	}
	r, err := request.Graph.Instantiate(request.Implementations)
	if err != nil {
		return err
	}
	ingress := map[string]bool{}
	for _, n := range s.Ingress {
		ingress[n] = true
	}
	types := map[string]string{}
	for _, p := range request.Graph.Places {
		types[p.Name] = p.Type
	}
	input := make(chan streamMessage)
	readErr := make(chan error, 1)
	go readStream(ctx, request.Input, input, readErr)
	encoder := json.NewEncoder(request.Output)
	transitions := append([]meshwork.AtomicTransition(nil), request.Graph.Transitions...)
	sort.Slice(transitions, func(i, j int) bool { return transitions[i].Name < transitions[j].Name })
	done := make(chan error, len(transitions))
	var pending *streamMessage
	for {
		for _, t := range transitions {
			a, err := r.Start(ctx, t.Name)
			if errors.Is(err, meshwork.ErrNotEnabled) {
				continue
			}
			if err != nil {
				return err
			}
			go func() { done <- a.Wait(ctx) }()
		}
		for _, n := range s.Egress {
			tok, ok := r.Token(n)
			if !ok {
				continue
			}
			if err := encoder.Encode(streamMessage{Place: n, Value: tok.Data}); err != nil {
				return err
			}
			if _, ok, err := r.Take(n); err != nil {
				return fmt.Errorf("stream output %s: %w", n, err)
			} else if !ok {
				return fmt.Errorf("stream output %s disappeared", n)
			}
		}
		if pending != nil {
			name, err := s.ResolveInput(pending.Place)
			if err != nil {
				return err
			}
			if !ingress[name] {
				return fmt.Errorf("stream stdin references non-input place %s", pending.Place)
			}
			tok, err := meshwork.NewToken(types[name], json.RawMessage(pending.Value))
			if err != nil {
				return fmt.Errorf("stream stdin place %s: %w", pending.Place, err)
			}
			if err := r.Put(name, tok); err == nil {
				pending = nil
			} else if !errors.Is(err, meshwork.ErrPlaceUnavailable) {
				return err
			}
		}
		changed := r.Changed()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
		case err := <-readErr:
			if err != nil {
				return err
			}
			readErr = nil
		case m, ok := <-input:
			if ok {
				pending = &m
			} else {
				input = nil
			}
		case <-changed:
		}
	}
}

type streamMessage struct {
	Place string          `json:"place"`
	Value json.RawMessage `json:"value"`
}

func readStream(ctx context.Context, in io.Reader, input chan<- streamMessage, readErr chan<- error) {
	defer close(input)
	d := json.NewDecoder(bufio.NewReader(in))
	for {
		var m streamMessage
		if err := d.Decode(&m); err == io.EOF {
			readErr <- nil
			return
		} else if err != nil {
			readErr <- fmt.Errorf("stream stdin: %w", err)
			return
		}
		if m.Place == "" || !json.Valid(m.Value) {
			readErr <- errors.New("stream stdin requires place and JSON value")
			return
		}
		select {
		case input <- m:
		case <-ctx.Done():
			return
		}
	}
}
