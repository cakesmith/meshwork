package meshwork

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Graph is the canonical, layout-independent Petri net, optionally still composed.
// References are composition declarations, never computational arcs.
type Graph struct {
	Stage       string `json:",omitempty"`
	Places      []Place
	Transitions []AtomicTransition
	References  []Reference `json:",omitempty"`
}
type Reference struct {
	Graph     string
	Namespace string
	Bindings  map[string]string
}
type Library struct{ Graphs map[string]Graph }
type AtomicTransition struct {
	Name                    string
	Implementation          string
	Inputs, Outputs         []string
	InputPorts, OutputPorts []string
	Timeout                 time.Duration `json:",omitempty"`
	Sources                 []string      `json:",omitempty"`
}

// ExecutableGraph retains the public name for flattened canonical graphs.
type ExecutableGraph = Graph

func (l Library) Compose(namespace string, graphs ...Graph) (ExecutableGraph, error) {
	var joined Graph
	for _, g := range graphs {
		joined.Places = append(joined.Places, g.Places...)
		joined.Transitions = append(joined.Transitions, g.Transitions...)
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

// Link resolves a graph's local identities and explicit reference bindings without
// expanding its children. Instance namespaces are always explicit.
func (l Library) Link(g Graph, namespace string) (Graph, error) {
	if namespace != "/" && !validName(namespace) {
		return Graph{}, fmt.Errorf("invalid namespace %q", namespace)
	}
	out := Graph{Stage: "linked"}
	resolve := func(n string) (string, error) { return Resolve(namespace, n) }
	for _, p := range g.Places {
		n, err := resolve(p.Name)
		if err != nil {
			return Graph{}, err
		}
		out.Places = append(out.Places, Place{n, p.Type})
	}
	for _, t := range g.Transitions {
		var err error
		t.Name, err = resolve(t.Name)
		if err != nil {
			return Graph{}, err
		}
		for direction, names := range [][]string{t.Inputs, t.Outputs} {
			resolved := make([]string, len(names))
			for i, n := range names {
				resolved[i], err = resolve(n)
				if err != nil {
					return Graph{}, err
				}
			}
			if direction == 0 {
				t.Inputs = resolved
			} else {
				t.Outputs = resolved
			}
		}
		out.Transitions = append(out.Transitions, t)
	}
	for _, ref := range g.References {
		var err error
		ref.Graph, err = resolve(ref.Graph)
		if err != nil {
			return Graph{}, err
		}
		if _, ok := l.Graphs[ref.Graph]; !ok {
			return Graph{}, fmt.Errorf("unknown graph %q", ref.Graph)
		}
		if ref.Namespace == "" {
			ref.Namespace = ref.Graph
		} else if ref.Namespace != "/" {
			ref.Namespace, err = resolve(ref.Namespace)
			if err != nil {
				return Graph{}, err
			}
		}
		bindings := map[string]string{}
		for k, v := range ref.Bindings {
			key, err := Resolve(ref.Graph, k)
			if err != nil {
				return Graph{}, err
			}
			value, err := resolve(v)
			if err != nil {
				return Graph{}, err
			}
			if old, ok := bindings[key]; ok && old != value {
				return Graph{}, fmt.Errorf("conflicting binding %s", key)
			}
			bindings[key] = value
		}
		ref.Bindings = bindings
		out.References = append(out.References, ref)
	}
	return normalizeGraph(out, false)
}

// Flatten expands finite graph references and merges compatible resolved objects.
func (l Library) Flatten(g Graph, namespace string) (ExecutableGraph, error) {
	stack := map[string]bool{}
	var expand func(Graph, string) (Graph, error)
	expand = func(g Graph, ns string) (Graph, error) {
		linked, err := l.Link(g, ns)
		if err != nil {
			return Graph{}, err
		}
		out := Graph{Places: linked.Places, Transitions: linked.Transitions}
		for _, ref := range linked.References {
			if stack[ref.Graph] {
				return Graph{}, fmt.Errorf("recursive graph reference %s", ref.Graph)
			}
			stack[ref.Graph] = true
			child, err := expand(l.Graphs[ref.Graph], ref.Graph)
			delete(stack, ref.Graph)
			if err != nil {
				return Graph{}, err
			}
			known := map[string]bool{}
			for _, p := range child.Places {
				known[p.Name] = true
			}
			for _, t := range child.Transitions {
				known[t.Name] = true
			}
			for k := range ref.Bindings {
				if !known[k] {
					return Graph{}, fmt.Errorf("binding names unknown object %s", k)
				}
			}
			remap := func(n string) string {
				if v, ok := ref.Bindings[n]; ok {
					return v
				}
				if strings.HasPrefix(n, strings.TrimSuffix(ref.Graph, "/")+"/") {
					return strings.TrimSuffix(ref.Namespace, "/") + strings.TrimPrefix(n, strings.TrimSuffix(ref.Graph, "/"))
				}
				return n
			}
			for _, p := range child.Places {
				p.Name = remap(p.Name)
				out.Places = append(out.Places, p)
			}
			for _, t := range child.Transitions {
				t.Name = remap(t.Name)
				for i := range t.Inputs {
					t.Inputs[i] = remap(t.Inputs[i])
				}
				for i := range t.Outputs {
					t.Outputs[i] = remap(t.Outputs[i])
				}
				t.Sources = append(append([]string(nil), t.Sources...), ref.Graph+"@"+ref.Namespace)
				out.Transitions = append(out.Transitions, t)
			}
		}
		return out, nil
	}
	expanded, err := expand(g, namespace)
	if err != nil {
		return Graph{}, err
	}
	return normalizeGraph(expanded, true)
}

func sortedPorts(names, ports []string) ([]string, []string) {
	order := make([]int, len(names))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return names[order[i]] < names[order[j]] })
	nn, pp := make([]string, len(names)), make([]string, len(ports))
	for i, j := range order {
		nn[i] = names[j]
		pp[i] = ports[j]
	}
	return nn, pp
}

func normalizeGraph(g Graph, requireEndpoints bool) (Graph, error) {
	ps := map[string]Place{}
	ts := map[string]AtomicTransition{}
	transitionNames := map[string]bool{}
	for _, t := range g.Transitions {
		transitionNames[t.Name] = true
	}
	for _, p := range g.Places {
		if !validName(p.Name) || p.Type == "" {
			return Graph{}, fmt.Errorf("invalid place %+v", p)
		}
		if old, ok := ps[p.Name]; ok && old.Type != p.Type {
			return Graph{}, fmt.Errorf("incompatible place declarations: %s", p.Name)
		}
		ps[p.Name] = p
	}
	for _, t := range g.Transitions {
		if !validName(t.Name) || t.Timeout < 0 {
			return Graph{}, fmt.Errorf("invalid transition %s", t.Name)
		}
		if _, ok := ps[t.Name]; ok {
			return Graph{}, fmt.Errorf("name used by place: %s", t.Name)
		}
		if !unique(t.Inputs) || !unique(t.Outputs) {
			return Graph{}, fmt.Errorf("%s: duplicate arc; use explicit fan-out places", t.Name)
		}
		if len(t.Inputs) != len(t.InputPorts) || len(t.Outputs) != len(t.OutputPorts) || !unique(t.InputPorts) || !unique(t.OutputPorts) {
			return Graph{}, fmt.Errorf("%s: invalid port mapping", t.Name)
		}
		for _, n := range append(append([]string(nil), t.Inputs...), t.Outputs...) {
			if !validName(n) {
				return Graph{}, fmt.Errorf("invalid arc endpoint %s", n)
			}
			if _, collision := transitionNames[n]; collision {
				return Graph{}, fmt.Errorf("non-bipartite arc %s -> %s", n, t.Name)
			}
			if _, ok := ps[n]; !ok && requireEndpoints {
				return Graph{}, fmt.Errorf("%s: unknown place %s", t.Name, n)
			}
		}
		t.Inputs, t.InputPorts = sortedPorts(t.Inputs, t.InputPorts)
		t.Outputs, t.OutputPorts = sortedPorts(t.Outputs, t.OutputPorts)
		sources := append([]string(nil), t.Sources...)
		t.Sources = nil
		if old, ok := ts[t.Name]; ok {
			sources = append(sources, old.Sources...)
			old.Sources = nil
			if !reflect.DeepEqual(old, t) {
				return Graph{}, fmt.Errorf("conflicting transition declarations: %s", t.Name)
			}
		}
		sort.Strings(sources)
		t.Sources = compact(sources)
		ts[t.Name] = t
	}
	result := Graph{Stage: g.Stage}
	refs := map[string]Reference{}
	for _, ref := range g.References {
		key := ref.Namespace + "\x00" + ref.Graph
		if old, ok := refs[key]; ok {
			if !reflect.DeepEqual(old.Bindings, ref.Bindings) {
				return Graph{}, fmt.Errorf("conflicting graph binding for %s", ref.Namespace)
			}
		} else {
			bindings := map[string]string{}
			for k, v := range ref.Bindings {
				bindings[k] = v
			}
			ref.Bindings = bindings
			refs[key] = ref
		}
	}
	keys := make([]string, 0, len(refs))
	for key := range refs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result.References = append(result.References, refs[key])
	}
	for _, p := range ps {
		result.Places = append(result.Places, p)
	}
	for _, t := range ts {
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
// Implementations receive local port names, independent of computational names.
func (g Graph) Instantiate(implementations map[string]Compute) (*Runtime, error) {
	return g.InstantiateWithClock(implementations, RealClock{})
}
func (g Graph) InstantiateWithClock(implementations map[string]Compute, clock Clock) (*Runtime, error) {
	if len(g.References) != 0 {
		return nil, fmt.Errorf("flatten graph references before instantiation")
	}
	r := NewWithClock(clock)
	for _, p := range g.Places {
		if err := r.AddPlace(p); err != nil {
			return nil, err
		}
	}
	for _, t := range g.Transitions {
		t := t
		if t.Implementation == "" {
			return nil, fmt.Errorf("%s: missing transition implementation", t.Name)
		}
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
