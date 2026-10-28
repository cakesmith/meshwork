package meshwork

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Graph is a layout-independent definition. References are metadata, not arcs.
type Graph struct {
	Places     []Place
	References []Reference
}

type Entity struct{ Inputs, Outputs []Place }
type Architecture struct {
	Entity         string
	Implementation string
	Graph          *Graph
	Timeout        time.Duration
}
type Reference struct {
	Name         string
	Architecture string
	Bindings     map[string]string
}
type Library struct {
	Entities      map[string]Entity
	Architectures map[string]Architecture
}
type AtomicTransition struct {
	Name                    string
	Implementation          string
	Inputs, Outputs         []string
	InputPorts, OutputPorts []string
	Timeout                 time.Duration
	Sources                 []string
}
type ExecutableGraph struct {
	Places      []Place
	Transitions []AtomicTransition
}

// Compose unifies declarations through the same resolver used for expansion.
func (l Library) Compose(namespace string, graphs ...Graph) (ExecutableGraph, error) {
	var joined Graph
	for _, g := range graphs {
		joined.Places = append(joined.Places, g.Places...)
		joined.References = append(joined.References, g.References...)
	}
	return l.Flatten(joined, namespace)
}

// Resolve never cleans malformed paths: equal resolved names mean equal objects.
func Resolve(namespace, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty computational name")
	}
	if !strings.HasPrefix(name, "/") {
		if namespace != "/" && !validName(namespace) {
			return "", fmt.Errorf("invalid namespace %q", namespace)
		}
		name = strings.TrimSuffix(namespace, "/") + "/" + name
	}
	if !validName(name) {
		return "", fmt.Errorf("invalid computational name %q", name)
	}
	return name, nil
}

func ports(e Entity) ([]Place, error) {
	all := append(append([]Place(nil), e.Inputs...), e.Outputs...)
	seen := map[string]bool{}
	if len(e.Inputs) == 0 || len(e.Outputs) == 0 {
		return nil, fmt.Errorf("entity requires inputs and outputs")
	}
	for _, p := range all {
		if !validName("/"+p.Name) || strings.Contains(p.Name, "/") || p.Type == "" || seen[p.Name] {
			return nil, fmt.Errorf("invalid or duplicate entity port %q", p.Name)
		}
		seen[p.Name] = true
	}
	return all, nil
}

// Flatten expands only finite, explicitly named references. Shared declarations
// must agree; there is no architecture ownership rule that copies shared places.
func (l Library) Flatten(g Graph, namespace string) (ExecutableGraph, error) {
	ps := map[string]Place{}
	ts := map[string]AtomicTransition{}
	stack := map[string]bool{}
	addPlace := func(p Place) error {
		if !validName(p.Name) || p.Type == "" {
			return fmt.Errorf("invalid place %+v", p)
		}
		if _, ok := ts[p.Name]; ok {
			return fmt.Errorf("name used by transition: %s", p.Name)
		}
		if old, ok := ps[p.Name]; ok && old.Type != p.Type {
			return fmt.Errorf("incompatible place declarations: %s", p.Name)
		}
		ps[p.Name] = p
		return nil
	}
	var expand func(Graph, string, map[string]string, string) error
	expand = func(g Graph, ns string, bound map[string]string, source string) error {
		declared := map[string]string{}
		resolve := func(n string) (string, error) {
			if v, ok := bound[n]; ok {
				return v, nil
			}
			return Resolve(ns, n)
		}
		for _, p := range g.Places {
			n, err := resolve(p.Name)
			if err != nil {
				return err
			}
			if err := addPlace(Place{n, p.Type}); err != nil {
				return err
			}
			declared[n] = p.Type
		}
		for _, ref := range g.References {
			name, err := Resolve(ns, ref.Name)
			if err != nil {
				return err
			}
			a, ok := l.Architectures[ref.Architecture]
			if !ok {
				return fmt.Errorf("%s: unknown architecture %q", name, ref.Architecture)
			}
			e, ok := l.Entities[a.Entity]
			if !ok {
				return fmt.Errorf("%s: unknown entity %q", name, a.Entity)
			}
			pp, err := ports(e)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if len(ref.Bindings) != len(pp) {
				return fmt.Errorf("%s: expected exactly %d port bindings", name, len(pp))
			}
			bindings := map[string]string{}
			for _, p := range pp {
				target, ok := ref.Bindings[p.Name]
				if !ok {
					return fmt.Errorf("%s: missing port %s", name, p.Name)
				}
				n, err := resolve(target)
				if err != nil {
					return err
				}
				if typ, ok := declared[n]; !ok || typ != p.Type {
					return fmt.Errorf("%s: port %s requires declared %s place %s in the referring graph", name, p.Name, p.Type, n)
				}
				bindings[p.Name] = n
			}
			path := source + " -> " + ref.Architecture + "@" + name
			if a.Timeout < 0 || (a.Graph == nil) == (a.Implementation == "") {
				return fmt.Errorf("%s: architecture must have exactly one implementation", name)
			}
			if a.Graph != nil {
				if a.Timeout != 0 {
					return fmt.Errorf("%s: composite timeout cannot be lowered to atomic timeouts", name)
				}
				if stack[ref.Architecture] {
					return fmt.Errorf("recursive architecture expansion: %s", path)
				}
				// Interface places must be declared explicitly in the implementation.
				for _, p := range pp {
					found := false
					for _, q := range a.Graph.Places {
						if q == p {
							found = true
						}
					}
					if !found {
						return fmt.Errorf("%s: implementation lacks interface place %s", name, p.Name)
					}
				}
				stack[ref.Architecture] = true
				err := expand(*a.Graph, name, bindings, path)
				delete(stack, ref.Architecture)
				if err != nil {
					return err
				}
				continue
			}
			t := AtomicTransition{Name: name, Implementation: a.Implementation, Timeout: a.Timeout}
			for _, p := range e.Inputs {
				t.Inputs = append(t.Inputs, bindings[p.Name])
				t.InputPorts = append(t.InputPorts, p.Name)
			}
			for _, p := range e.Outputs {
				t.Outputs = append(t.Outputs, bindings[p.Name])
				t.OutputPorts = append(t.OutputPorts, p.Name)
			}
			if !unique(t.Inputs) || !unique(t.Outputs) {
				return fmt.Errorf("%s: duplicate arc; use explicit fan-out places", name)
			}
			if _, ok := ps[name]; ok {
				return fmt.Errorf("name used by place: %s", name)
			}
			if old, ok := ts[name]; ok {
				sources := old.Sources
				old.Sources = nil
				if !reflect.DeepEqual(old, t) {
					return fmt.Errorf("conflicting transition declarations: %s", name)
				}
				t.Sources = sources
			}
			t.Sources = append(t.Sources, path)
			ts[name] = t
		}
		return nil
	}
	if namespace != "/" && !validName(namespace) {
		return ExecutableGraph{}, fmt.Errorf("invalid namespace %q", namespace)
	}
	// Predeclare every level before expanding references so declaration order is irrelevant.
	if err := expand(g, namespace, nil, "root"); err != nil {
		return ExecutableGraph{}, err
	}
	result := ExecutableGraph{}
	for _, p := range ps {
		result.Places = append(result.Places, p)
	}
	for _, t := range ts {
		sort.Strings(t.Sources)
		t.Sources = compact(t.Sources)
		result.Transitions = append(result.Transitions, t)
	}
	sort.Slice(result.Places, func(i, j int) bool { return result.Places[i].Name < result.Places[j].Name })
	sort.Slice(result.Transitions, func(i, j int) bool { return result.Transitions[i].Name < result.Transitions[j].Name })
	return result, nil
}

func compact(ss []string) []string {
	out := ss[:0]
	for _, s := range ss {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}

// Instantiate validates a serialized executable graph before returning a runtime.
// Implementations receive entity port names, independent of computational names.
func (g ExecutableGraph) Instantiate(implementations map[string]Compute) (*Runtime, error) {
	r := New()
	for _, p := range g.Places {
		if err := r.AddPlace(p); err != nil {
			return nil, err
		}
	}
	for _, t := range g.Transitions {
		t := t
		t.Inputs = append([]string(nil), t.Inputs...)
		t.Outputs = append([]string(nil), t.Outputs...)
		t.InputPorts = append([]string(nil), t.InputPorts...)
		t.OutputPorts = append([]string(nil), t.OutputPorts...)
		fn := implementations[t.Implementation]
		if fn == nil {
			return nil, fmt.Errorf("%s: unknown implementation %q", t.Name, t.Implementation)
		}
		if len(t.Inputs) != len(t.InputPorts) || len(t.Outputs) != len(t.OutputPorts) || !unique(t.InputPorts) || !unique(t.OutputPorts) {
			return nil, fmt.Errorf("%s: invalid port mapping", t.Name)
		}
		run := func(ctx context.Context, in map[string]Token) (map[string]Token, error) {
			local := map[string]Token{}
			for i, n := range t.Inputs {
				local[t.InputPorts[i]] = in[n]
			}
			out, err := fn(ctx, local)
			if err != nil {
				return nil, err
			}
			if len(out) != len(t.Outputs) {
				return nil, fmt.Errorf("%s: wrong output count", t.Name)
			}
			result := map[string]Token{}
			for i, n := range t.Outputs {
				tok, ok := out[t.OutputPorts[i]]
				if !ok {
					return nil, fmt.Errorf("%s: missing output port %s", t.Name, t.OutputPorts[i])
				}
				result[n] = tok
			}
			return result, nil
		}
		if err := r.AddTransition(Transition{Name: t.Name, Inputs: t.Inputs, Outputs: t.Outputs, Timeout: t.Timeout, Run: run}); err != nil {
			return nil, err
		}
	}
	return r, nil
}
