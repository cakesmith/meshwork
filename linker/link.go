package linker

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"example.com/meshwork-prototype/meshwork"
)

type declaration struct {
	Name, Kind, Type, Implementation, Source string
	Timeout                                  time.Duration
}
type arc struct{ From, To, Port, Source string }
type region struct {
	Sources []*source
}
type contribution struct {
	Nodes []declaration
	Arcs  []arc
}

type Result struct {
	Sources     map[string][]string `json:",omitempty"`
	Graph       meshwork.ExecutableGraph
	Definitions map[string]meshwork.ExecutableGraph
	Scenarios   []Scenario
	Diagnostics []string          `json:",omitempty"`
	Imports     []GraphMLDocument `json:",omitempty"`
}

// Link discovers all files before resolving any references.
func Link(repos []Repository) (Result, error) {
	files, err := Discover(repos)
	if err != nil {
		return Result{}, err
	}
	return LinkFiles(files)
}

// LinkFiles also permits callers to supply a previously discovered file list.
// Input order has no effect on canonical output or diagnostics.
func LinkFiles(files []File) (Result, error) {
	return linkFiles(files, false)
}

func linkFiles(files []File, allowIncomplete bool) (Result, error) {
	files = append([]File(nil), files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	regions := map[string]*region{}
	handles := map[string]string{}
	handleSources := map[string]string{}
	var sources []*source
	var problems []error
	for _, f := range files {
		s, err := parse(f)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		sources = append(sources, s)
		if s.Imported != nil {
			allowIncomplete = true
		}
		if f.Kind != "graph" && s.Imported == nil {
			continue
		}
		h := f.Repository.Name + ":" + f.Name
		if old, ok := handles[h]; ok && old != s.NS {
			problems = append(problems, fmt.Errorf("%s and %s: ambiguous definition %s: %s versus %s", handleSources[h], s.loc("namespace", ""), h, old, s.NS))
			continue
		}
		handles[h] = s.NS
		handleSources[h] = s.loc("namespace", "")
		r := regions[s.NS]
		if r == nil {
			r = &region{}
			regions[s.NS] = r
		}
		r.Sources = append(r.Sources, s)
	}
	resolveGraph := func(s *source, name string) (string, error) {
		if strings.Contains(name, ":") {
			if ns, ok := handles[name]; ok {
				return ns, nil
			}
			return "", fmt.Errorf("%s: unresolved repository definition %q", s.loc("graph", ""), name)
		}
		if ns, ok := handles[s.File.Repository.Name+":"+name]; ok {
			return ns, nil
		}
		n, err := resolveName(s.NS, name)
		if err == nil && regions[n] != nil {
			return n, nil
		}
		if strings.Contains(name, ".") && !strings.Contains(name, "/") {
			n = "/" + strings.ReplaceAll(name, ".", "/")
			if regions[n] != nil {
				return n, nil
			}
		}
		return "", fmt.Errorf("%s: unresolved graph %q (context %s)", s.loc("graph", ""), name, s.NS)
	}
	var expand func(string, map[string]bool) (contribution, error)
	expand = func(ns string, stack map[string]bool) (contribution, error) {
		if stack[ns] {
			return contribution{}, fmt.Errorf("recursive graph reference to %s", ns)
		}
		stack[ns] = true
		defer delete(stack, ns)
		var c contribution
		for _, s := range regions[ns].Sources {
			resolve := func(n string) (string, error) { return boundName(s, n) }
			locations := map[string][]string{}
			if s.Imported != nil {
				for _, p := range s.Imported.Provenance {
					locations[p.Identity] = append(locations[p.Identity], s.File.Path+"#"+p.ID)
				}
			}
			addNode := func(n declaration, original string) {
				if refs := locations[original]; len(refs) > 0 {
					for _, ref := range refs {
						n.Source = ref
						c.Nodes = append(c.Nodes, n)
					}
				} else {
					c.Nodes = append(c.Nodes, n)
				}
			}
			for _, name := range sortedKeys(s.Doc.Places) {
				n, err := resolve(name)
				if err != nil {
					return c, fmt.Errorf("%s: %w", s.loc("places", name), err)
				}
				addNode(declaration{Name: n, Kind: "place", Type: s.Doc.Places[name], Source: s.loc("places", name)}, name)
			}
			for _, name := range sortedKeys(s.Doc.Transitions) {
				n, err := resolve(name)
				if err != nil {
					return c, fmt.Errorf("%s: %w", s.loc("transitions", name), err)
				}
				impl, err := implementation(s.Doc.Transitions[name].Implementation)
				if err != nil && !(allowIncomplete && s.Doc.Transitions[name].Implementation == "") {
					return c, fmt.Errorf("%s: %s: %w", s.loc("transitions", name), n, err)
				}
				t := s.Doc.Transitions[name]
				addNode(declaration{Name: n, Kind: "transition", Implementation: impl, Timeout: time.Duration(t.Timeout), Source: s.loc("transitions", name)}, name)
			}
			for index, a := range s.Doc.Arcs {
				if len(a) != 2 {
					return c, fmt.Errorf("%s: arc must have two endpoints: %v", s.loc("arcs", ""), a)
				}
				from, err := resolve(a[0])
				if err != nil {
					return c, fmt.Errorf("%s: %w", s.loc("arcs", ""), err)
				}
				to, err := resolve(a[1])
				if err != nil {
					return c, fmt.Errorf("%s: %w", s.loc("arcs", ""), err)
				}
				// Port names belong to the definition, before occurrence bindings.
				location := s.loc("arcs", "")
				if s.Imported != nil {
					location = s.File.Path + "#" + s.Imported.ArcIDs[index]
				}
				c.Arcs = append(c.Arcs, arc{From: from, To: to, Port: port(from) + "\x00" + port(to), Source: location})
			}
			for _, ref := range s.Doc.References {
				location := s.loc("references", "")
				target, err := resolveGraph(s, ref.Graph)
				if err != nil {
					return c, fmt.Errorf("%s: %w", location, err)
				}
				child, err := expand(target, stack)
				if err != nil {
					return c, fmt.Errorf("%s: %w", s.loc("references", ""), err)
				}
				bindings := map[string]string{}
				for _, k := range sortedKeys(ref.Bindings) {
					a, err := resolveName(target, k)
					if err != nil {
						return c, fmt.Errorf("%s: %w", location, err)
					}
					b, err := resolve(ref.Bindings[k])
					if err != nil {
						return c, fmt.Errorf("%s: %w", location, err)
					}
					if old, ok := bindings[a]; ok && old != b {
						return c, fmt.Errorf("%s: conflicting binding %s: %s versus %s", s.loc("references", ""), a, old, b)
					}
					bindings[a] = b
				}
				known := map[string]bool{}
				for _, n := range child.Nodes {
					known[n.Name] = true
				}
				for _, k := range sortedKeys(bindings) {
					if !known[k] {
						return c, fmt.Errorf("%s: binding names unknown object %s", s.loc("references", ""), k)
					}
				}
				mount := target
				if ref.Namespace != "" {
					mount, err = resolveName(s.NS, ref.Namespace)
					if err != nil {
						return c, fmt.Errorf("%s: %w", location, err)
					}
				}
				remap := func(n string) string {
					if b, ok := bindings[n]; ok {
						return b
					}
					if strings.HasPrefix(n, strings.TrimSuffix(target, "/")+"/") {
						return strings.TrimSuffix(mount, "/") + strings.TrimPrefix(n, strings.TrimSuffix(target, "/"))
					}
					return n
				}
				for _, n := range child.Nodes {
					n.Name = remap(n.Name)
					c.Nodes = append(c.Nodes, n)
				}
				for _, a := range child.Arcs {
					a.From = remap(a.From)
					a.To = remap(a.To)
					c.Arcs = append(c.Arcs, a)
				}
			}
		}
		return c, nil
	}
	all := contribution{}
	members := map[string]contribution{}
	for _, ns := range sortedKeys(regions) {
		c, err := expand(ns, map[string]bool{})
		if err != nil {
			problems = append(problems, err)
			continue
		}
		members[ns] = c
		all.Nodes = append(all.Nodes, c.Nodes...)
		all.Arcs = append(all.Arcs, c.Arcs...)
	}
	g, err := canonical(all, allowIncomplete)
	if err != nil {
		problems = append(problems, err)
	}
	if len(problems) > 0 {
		return Result{}, diagnostics(problems)
	}
	result := Result{Graph: g, Definitions: map[string]meshwork.ExecutableGraph{}}
	result.Sources = map[string][]string{}
	for _, n := range all.Nodes {
		result.Sources[n.Name] = append(result.Sources[n.Name], n.Source)
	}
	for _, a := range all.Arcs {
		id := a.From + " -> " + a.To
		result.Sources[id] = append(result.Sources[id], a.Source)
	}
	for id, locations := range result.Sources {
		sort.Strings(locations)
		result.Sources[id] = dedup(locations)
	}
	result.Diagnostics = incomplete(g, all.Nodes)
	for _, s := range sources {
		if s.Imported != nil {
			result.Imports = append(result.Imports, *s.Imported)
		}
	}
	for _, ns := range sortedKeys(members) {
		// Select final merged objects, including additions from overlapping regions.
		set := map[string]bool{}
		for _, n := range members[ns].Nodes {
			set[n.Name] = true
		}
		for _, a := range members[ns].Arcs {
			set[a.From], set[a.To] = true, true
		}
		for _, t := range g.Transitions {
			if set[t.Name] {
				for _, p := range append(append([]string(nil), t.Inputs...), t.Outputs...) {
					set[p] = true
				}
			}
		}
		var sub meshwork.ExecutableGraph
		for _, p := range g.Places {
			if set[p.Name] {
				sub.Places = append(sub.Places, p)
			}
		}
		for _, t := range g.Transitions {
			if set[t.Name] {
				sub.Transitions = append(sub.Transitions, t)
			}
		}
		result.Definitions[ns] = sub
	}
	for _, h := range sortedKeys(handles) {
		result.Definitions[h] = result.Definitions[handles[h]]
	}
	for _, s := range sources {
		if s.File.Kind != "graph" && s.Imported == nil {
			ns, err := resolveGraph(s, s.Doc.Graph)
			if err != nil {
				problems = append(problems, err)
				continue
			}
			cases, err := resolveScenarios(s, ns, result.Definitions[ns])
			if err != nil {
				problems = append(problems, err)
				continue
			}
			result.Scenarios = append(result.Scenarios, cases...)
		}
	}
	if len(problems) > 0 {
		return Result{}, diagnostics(problems)
	}
	return result, nil
}

func boundName(s *source, n string) (string, error) {
	name, err := resolveName(s.NS, n)
	if err != nil {
		return "", err
	}
	bindings := map[string]string{}
	for _, k := range sortedKeys(s.Doc.Bindings) {
		a, err := resolveName(s.NS, k)
		if err != nil {
			return "", err
		}
		b, err := resolveName(s.NS, s.Doc.Bindings[k])
		if err != nil {
			return "", err
		}
		if old, ok := bindings[a]; ok && old != b {
			return "", fmt.Errorf("conflicting binding %s: %s versus %s", a, old, b)
		}
		bindings[a] = b
	}
	if b, ok := bindings[name]; ok {
		return b, nil
	}
	return name, nil
}

func canonical(c contribution, allowIncomplete bool) (meshwork.ExecutableGraph, error) {
	nodes := map[string]declaration{}
	sources := map[string][]string{}
	var problems []error
	for _, n := range c.Nodes {
		if n.Name == "/" || (!allowIncomplete && n.Type == "" && n.Implementation == "") {
			problems = append(problems, fmt.Errorf("%s: invalid declaration %s", n.Source, n.Name))
			continue
		}
		if old, ok := nodes[n.Name]; ok {
			if old.Kind != n.Kind || (old.Type != "" && n.Type != "" && old.Type != n.Type) || (old.Implementation != "" && n.Implementation != "" && old.Implementation != n.Implementation) || old.Timeout != n.Timeout {
				problems = append(problems, fmt.Errorf("%s and %s: conflicting identity %s: {kind:%q type:%q implementation:%q timeout:%s} versus {kind:%q type:%q implementation:%q timeout:%s}", old.Source, n.Source, n.Name, old.Kind, old.Type, old.Implementation, old.Timeout, n.Kind, n.Type, n.Implementation, n.Timeout))
			}
			if old.Type == "" {
				old.Type = n.Type
			}
			if old.Implementation == "" {
				old.Implementation = n.Implementation
			}
			nodes[n.Name] = old
		} else {
			nodes[n.Name] = n
		}
		sources[n.Name] = append(sources[n.Name], n.Source)
	}
	ts := map[string]*meshwork.AtomicTransition{}
	var g meshwork.ExecutableGraph
	for _, name := range sortedKeys(nodes) {
		n := nodes[name]
		if n.Kind == "place" {
			g.Places = append(g.Places, meshwork.Place{Name: name, Type: n.Type})
		} else {
			sort.Strings(sources[name])
			ts[name] = &meshwork.AtomicTransition{Name: name, Implementation: n.Implementation, Timeout: n.Timeout, Sources: dedup(sources[name])}
		}
	}
	sort.Slice(c.Arcs, func(i, j int) bool {
		a, b := c.Arcs[i], c.Arcs[j]
		return a.From+"\x00"+a.To+"\x00"+a.Source < b.From+"\x00"+b.To+"\x00"+b.Source
	})
	seen := map[string]string{}
	for _, a := range c.Arcs {
		from, fok := nodes[a.From]
		to, tok := nodes[a.To]
		if !fok || !tok {
			problems = append(problems, fmt.Errorf("%s: unresolved arc %s -> %s", a.Source, a.From, a.To))
			continue
		}
		if from.Kind == to.Kind {
			problems = append(problems, fmt.Errorf("%s: non-bipartite arc %s -> %s", a.Source, a.From, a.To))
			continue
		}
		pp := strings.Split(a.Port, "\x00")
		key := a.From + "\x00" + a.To
		p := pp[0]
		if from.Kind == "transition" {
			p = pp[1]
		}
		if old, ok := seen[key]; ok {
			if old != p {
				problems = append(problems, fmt.Errorf("%s: conflicting ports for arc %s -> %s: %s versus %s", a.Source, a.From, a.To, old, p))
			}
			continue
		}
		seen[key] = p
		if from.Kind == "place" {
			t := ts[a.To]
			t.Inputs = append(t.Inputs, a.From)
			t.InputPorts = append(t.InputPorts, p)
		} else {
			t := ts[a.From]
			t.Outputs = append(t.Outputs, a.To)
			t.OutputPorts = append(t.OutputPorts, p)
		}
	}
	for _, name := range sortedKeys(ts) {
		t := ts[name]
		if (!allowIncomplete && (len(t.Inputs) == 0 || len(t.Outputs) == 0)) || !isUnique(t.InputPorts) || !isUnique(t.OutputPorts) {
			problems = append(problems, fmt.Errorf("%s: %s requires nonempty inputs/outputs with unique local port names", strings.Join(t.Sources, ", "), name))
		}
		g.Transitions = append(g.Transitions, *t)
	}
	if len(problems) > 0 {
		return meshwork.ExecutableGraph{}, diagnostics(problems)
	}
	return g, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func isUnique(ss []string) bool {
	m := map[string]bool{}
	for _, s := range ss {
		if s == "" || m[s] {
			return false
		}
		m[s] = true
	}
	return true
}
func dedup(ss []string) []string {
	out := ss[:0]
	for _, s := range ss {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}
func diagnostics(es []error) error {
	ss := make([]string, 0, len(es))
	for _, e := range es {
		ss = append(ss, e.Error())
	}
	sort.Strings(ss)
	ss = dedup(ss)
	return errors.New(strings.Join(ss, "\n"))
}
