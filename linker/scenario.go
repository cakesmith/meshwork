package linker

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"example.com/meshwork-prototype/meshwork"
)

type Scenario struct {
	Name, Graph, Source string
	Marking, Expect     map[string]meshwork.Token
	Clock               ClockSpec
	Steps               []Step
	Input, Output       string
	Ingress, Egress     []string
	inputNamespace      string
	inputBindings       map[string]string
}

type ClockSpec struct {
	Type  string
	Start time.Duration
}
type Step struct {
	AdvanceClock *time.Duration
	Expect       map[string]meshwork.Token
	States       map[string]string
}

func validateClock(c ClockSpec) error {
	if c.Type != "" && c.Type != "real" && c.Type != "manual" {
		return fmt.Errorf("unknown clock type %q", c.Type)
	}
	if c.Start < 0 || (c.Start != 0 && c.Type != "manual") {
		return fmt.Errorf("clock start requires a manual clock and nonnegative offset")
	}
	return nil
}

func resolveScenarios(s *source, ns string, g meshwork.ExecutableGraph) ([]Scenario, error) {
	location := s.loc("scenarios", "")
	cases := s.Doc.Scenarios
	if s.File.Kind == "scenario" {
		cases = []scenario{{Name: s.Doc.Name, Input: s.Doc.Input, Output: s.Doc.Output, Marking: s.Doc.Marking, Expect: s.Doc.Expect, Clock: s.Doc.Clock, Steps: s.Doc.Steps}}
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s: scenario collection must not be empty", s.loc("scenarios", ""))
	}
	types := map[string]string{}
	for _, p := range g.Places {
		types[p.Name] = p.Type
	}
	seen := map[string]bool{}
	var result []Scenario
	local := *s
	local.NS = ns
	if _, err := boundName(&local, "/"); err != nil {
		return nil, fmt.Errorf("%s: %w", s.loc("bindings", ""), err)
	}
	for _, c := range cases {
		if c.Name == "" || seen[c.Name] {
			return nil, fmt.Errorf("%s: empty or duplicate scenario name %q", s.loc("scenarios", ""), c.Name)
		}
		if c.Clock.Type == "" && c.Clock.Start == 0 {
			c.Clock = s.Doc.Clock
		}
		config := ClockSpec{Type: c.Clock.Type, Start: time.Duration(c.Clock.Start)}
		if err := validateClock(config); err != nil {
			return nil, fmt.Errorf("%s: scenario %s: %w", location, c.Name, err)
		}
		if len(c.Steps) > 0 && config.Type != "manual" {
			return nil, fmt.Errorf("%s: scenario steps require a manual clock", location)
		}
		seen[c.Name] = true
		if c.Input != "" || c.Output != "" {
			if s.File.Kind != "scenario" || c.Input != "stdin" || c.Output != "stdout" || c.Marking != nil || c.Expect != nil || len(c.Steps) != 0 || config.Type == "manual" {
				return nil, fmt.Errorf("%s: streaming scenarios require singular input: stdin, output: stdout, and no marking, expect, or steps", location)
			}
		}
		item := Scenario{Clock: config, Name: c.Name, Graph: ns, Source: location, Marking: map[string]meshwork.Token{}, Expect: map[string]meshwork.Token{}, Input: c.Input, Output: c.Output}
		if c.Input == "stdin" {
			item.inputNamespace, item.inputBindings = ns, s.Doc.Bindings
			producers, consumers := map[string]bool{}, map[string]bool{}
			for _, t := range g.Transitions {
				for _, n := range t.Inputs {
					consumers[n] = true
				}
				for _, n := range t.Outputs {
					producers[n] = true
				}
			}
			for _, p := range g.Places {
				if !producers[p.Name] {
					item.Ingress = append(item.Ingress, p.Name)
				}
				if !consumers[p.Name] {
					item.Egress = append(item.Egress, p.Name)
				}
			}
			sort.Strings(item.Ingress)
			sort.Strings(item.Egress)
			if len(item.Ingress) == 0 || len(item.Egress) == 0 {
				return nil, fmt.Errorf("%s: streaming scenario requires graph source and sink places", location)
			}
		}
		if c.Output != "" && c.Output != "stdout" {
			n, err := boundName(&local, c.Output)
			if err != nil {
				return nil, fmt.Errorf("%s: scenario %s: %w", location, c.Name, err)
			}
			if types[n] == "" {
				return nil, fmt.Errorf("%s: scenario %s references unknown output place %s", location, c.Name, n)
			}
			item.Output = n
		}
		for i, values := range []map[string]any{c.Marking, c.Expect} {
			target := item.Marking
			if i == 1 {
				target = item.Expect
			}
			for _, k := range sortedKeys(values) {
				n, err := boundName(&local, k)
				if err != nil {
					return nil, fmt.Errorf("%s: scenario %s: %w", location, c.Name, err)
				}
				if types[n] == "" {
					return nil, fmt.Errorf("%s: scenario %s references unknown place %s", location, c.Name, n)
				}
				if _, ok := target[n]; ok {
					return nil, fmt.Errorf("%s: scenario %s resolves multiple values to %s", location, c.Name, n)
				}
				tok, err := meshwork.NewToken(types[n], values[k])
				if err != nil {
					return nil, fmt.Errorf("%s: scenario %s place %s: %w", location, c.Name, n, err)
				}
				target[n] = tok
			}
		}
		for _, raw := range c.Steps {
			step := Step{}
			if raw.Expect != nil {
				step.Expect = map[string]meshwork.Token{}
				step.States = map[string]string{}
			}
			if (raw.AdvanceClock == nil) == (raw.Expect == nil) {
				return nil, fmt.Errorf("%s: each step requires exactly one advance_clock or expect", location)
			}
			if raw.AdvanceClock != nil {
				d := time.Duration(*raw.AdvanceClock)
				if d < 0 {
					return nil, fmt.Errorf("manual clock cannot move backwards")
				}
				step.AdvanceClock = &d
			}
			for _, k := range sortedKeys(raw.Expect) {
				n, err := boundName(&local, k)
				if err != nil {
					return nil, err
				}
				if types[n] == "" {
					return nil, fmt.Errorf("%s: step references unknown place %s", location, n)
				}
				if _, ok := step.Expect[n]; ok {
					return nil, fmt.Errorf("duplicate step expectation %s", n)
				}
				if _, ok := step.States[n]; ok {
					return nil, fmt.Errorf("duplicate step expectation %s", n)
				}
				if state, ok := raw.Expect[k].(string); ok && (state == "empty" || state == "present") {
					step.States[n] = state
					continue
				}
				tok, err := meshwork.NewToken(types[n], raw.Expect[k])
				if err != nil {
					return nil, err
				}
				step.Expect[n] = tok
			}
			item.Steps = append(item.Steps, step)
		}
		result = append(result, item)
	}
	return result, nil
}

// ResolveInput resolves a stream frame's place name through this scenario's bindings.
func (s Scenario) ResolveInput(name string) (string, error) {
	return boundName(&source{NS: s.inputNamespace, Doc: document{Bindings: s.inputBindings}}, name)
}

// RunScenario runs a finite example on an isolated marking. Each transition fires
// once; cyclic/reactive graphs require a future explicit scheduling policy.
func RunScenario(ctx context.Context, g meshwork.ExecutableGraph, s Scenario, impl map[string]meshwork.Compute) (*meshwork.Runtime, error) {
	return RunScenarioWithRuntime(ctx, g, s, func(clock meshwork.Clock) (*meshwork.Runtime, error) { return g.InstantiateWithClock(impl, clock) })
}

// RunScenarioWithRuntime executes against a host-supplied runtime factory.
func RunScenarioWithRuntime(ctx context.Context, g meshwork.Graph, s Scenario, instantiate func(meshwork.Clock) (*meshwork.Runtime, error)) (*meshwork.Runtime, error) {
	if err := validateClock(s.Clock); err != nil {
		return nil, err
	}
	if s.Clock.Type == "manual" {
		return runManualScenario(ctx, g, s, instantiate)
	}
	if len(s.Steps) > 0 {
		return nil, fmt.Errorf("scenario steps require a manual clock")
	}
	r, err := instantiate(meshwork.RealClock{})
	if err != nil {
		return nil, err
	}
	for _, n := range sortedKeys(s.Marking) {
		if err := r.Put(n, s.Marking[n]); err != nil {
			return nil, err
		}
	}
	if expectationsReady(r, s.Expect) {
		return r, checkExpect(r, s, s.Expect, nil)
	}
	pending := append([]meshwork.AtomicTransition(nil), g.Transitions...)
	for len(pending) > 0 {
		next := make([]meshwork.AtomicTransition, 0, len(pending))
		for _, t := range pending {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			err := r.Fire(ctx, t.Name)
			if errors.Is(err, meshwork.ErrNotEnabled) {
				next = append(next, t)
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("%s: scenario %s: %w", s.Source, s.Name, err)
			}
			if expectationsReady(r, s.Expect) {
				return r, checkExpect(r, s, s.Expect, nil)
			}
		}
		if len(next) == len(pending) {
			return nil, fmt.Errorf("%s: scenario %s stalled with %d transitions pending", s.Source, s.Name, len(next))
		}
		pending = next
	}
	if err := checkExpect(r, s, s.Expect, nil); err != nil {
		return nil, err
	}
	return r, nil
}

func checkExpect(r *meshwork.Runtime, s Scenario, values map[string]meshwork.Token, states map[string]string) error {
	for _, n := range sortedKeys(values) {
		tok, ok := r.Token(n)
		if !ok || tok.Hash != values[n].Hash {
			return fmt.Errorf("%s: scenario %s: expectation failed for %s: got %s, want %s", s.Source, s.Name, n, tok.Data, values[n].Data)
		}
	}
	known := map[string]bool{}
	if len(states) > 0 {
		for _, p := range r.Snapshot() {
			known[p.Name] = true
		}
	}
	for _, n := range sortedKeys(states) {
		if !known[n] || (states[n] != "empty" && states[n] != "present") {
			return fmt.Errorf("invalid state expectation for %s", n)
		}
		_, present := r.Token(n)
		if present != (states[n] == "present") {
			return fmt.Errorf("%s: scenario %s: expectation failed for %s: want %s", s.Source, s.Name, n, states[n])
		}
	}
	return nil
}

func expectationsReady(r *meshwork.Runtime, values map[string]meshwork.Token) bool {
	if len(values) == 0 {
		return false
	}
	for _, n := range sortedKeys(values) {
		if _, ok := r.Token(n); !ok {
			return false
		}
	}
	return true
}

func runManualScenario(parent context.Context, g meshwork.Graph, s Scenario, instantiate func(meshwork.Clock) (*meshwork.Runtime, error)) (*meshwork.Runtime, error) {
	ctx, cancel := context.WithCancel(parent)
	clock := meshwork.NewManualClock(time.Unix(0, 0).Add(s.Clock.Start))
	r, err := instantiate(clock)
	if err != nil {
		cancel()
		return nil, err
	}
	active := map[string]*meshwork.Attempt{}
	// Step completion is a stopping condition; never leave reserved tokens behind.
	defer func() {
		cancel()
		for _, a := range active {
			_ = a.Wait(context.Background())
		}
	}()
	for _, n := range sortedKeys(s.Marking) {
		if err := r.Put(n, s.Marking[n]); err != nil {
			return nil, err
		}
	}
	pending := append([]meshwork.AtomicTransition(nil), g.Transitions...)
	sort.Slice(pending, func(i, j int) bool { return pending[i].Name < pending[j].Name })
	drain := func() error {
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			next := make([]meshwork.AtomicTransition, 0, len(pending))
			progress := false
			for _, t := range pending {
				a, err := r.Start(ctx, t.Name)
				if errors.Is(err, meshwork.ErrNotEnabled) {
					next = append(next, t)
					continue
				}
				if err != nil {
					return err
				}
				active[t.Name] = a
				progress = true
			}
			pending = next
			if err := r.Settle(ctx); err != nil {
				return err
			}
			for _, name := range sortedKeys(active) {
				a := active[name]
				select {
				case <-a.Done():
					if err := a.Wait(ctx); err != nil {
						return fmt.Errorf("%s: scenario %s transition %s: %w", s.Source, s.Name, name, err)
					}
					delete(active, name)
					progress = true
				default:
				}
			}
			if !progress {
				return nil
			}
		}
	}
	if err := drain(); err != nil {
		return nil, err
	}
	for _, step := range s.Steps {
		if (step.AdvanceClock == nil) == (step.Expect == nil && step.States == nil) {
			return nil, fmt.Errorf("each step requires exactly one advance_clock or expect")
		}
		if step.AdvanceClock != nil {
			if err := clock.Advance(*step.AdvanceClock); err != nil {
				return nil, err
			}
		}
		if err := drain(); err != nil {
			return nil, err
		}
		if err := checkExpect(r, s, step.Expect, step.States); err != nil {
			return nil, err
		}
	}
	if len(s.Steps) == 0 && (len(pending) > 0 || len(active) > 0) {
		return nil, fmt.Errorf("%s: scenario %s stalled; manual clock requires steps", s.Source, s.Name)
	}
	if err := checkExpect(r, s, s.Expect, nil); err != nil {
		return nil, err
	}
	return r, nil
}
