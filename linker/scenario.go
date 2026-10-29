package linker

import (
	"context"
	"errors"
	"fmt"

	"example.com/meshwork-prototype/meshwork"
)

type Scenario struct {
	Name, Graph, Source string
	Marking, Expect     map[string]meshwork.Token
}

func resolveScenarios(s *source, ns string, g meshwork.ExecutableGraph) ([]Scenario, error) {
	location := s.loc("scenarios", "")
	cases := s.Doc.Scenarios
	if s.File.Kind == "scenario" {
		cases = []scenario{{s.Doc.Name, s.Doc.Marking, s.Doc.Expect}}
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
		seen[c.Name] = true
		item := Scenario{Name: c.Name, Graph: ns, Source: location, Marking: map[string]meshwork.Token{}, Expect: map[string]meshwork.Token{}}
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
		result = append(result, item)
	}
	return result, nil
}

// RunScenario runs a finite example on an isolated marking. Each transition fires
// once; cyclic/reactive graphs require a future explicit scheduling policy.
func RunScenario(ctx context.Context, g meshwork.ExecutableGraph, s Scenario, impl map[string]meshwork.Compute) (*meshwork.Runtime, error) {
	r, err := g.Instantiate(impl)
	if err != nil {
		return nil, err
	}
	for _, n := range sortedKeys(s.Marking) {
		if err := r.Put(n, s.Marking[n]); err != nil {
			return nil, err
		}
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
		}
		if len(next) == len(pending) {
			return nil, fmt.Errorf("%s: scenario %s stalled with %d transitions pending", s.Source, s.Name, len(next))
		}
		pending = next
	}
	for _, n := range sortedKeys(s.Expect) {
		tok, ok := r.Token(n)
		if !ok || tok.Hash != s.Expect[n].Hash {
			return nil, fmt.Errorf("%s: scenario %s: expectation failed for %s: got %s, want %s", s.Source, s.Name, n, tok.Data, s.Expect[n].Data)
		}
	}
	return r, nil
}
