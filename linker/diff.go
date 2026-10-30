package linker

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"

	"example.com/meshwork-prototype/meshwork"
)

// Value contains only canonical computational attributes. Arcs carry local port names.
type Value struct {
	Type           string            `json:"type,omitempty"`
	Implementation string            `json:"implementation,omitempty"`
	Graph          string            `json:"graph,omitempty"`
	Namespace      string            `json:"namespace,omitempty"`
	Bindings       map[string]string `json:"bindings,omitempty"`
	Timeout        time.Duration     `json:"timeout,omitempty"`
	From           string            `json:"from,omitempty"`
	To             string            `json:"to,omitempty"`
	Transport      string            `json:"transport,omitempty"`
	Port           string            `json:"port,omitempty"`
}
type Change struct {
	Kind          string   `json:"kind"`
	Element       string   `json:"element"`
	Identity      string   `json:"identity"`
	Before        *Value   `json:"before,omitempty"`
	After         *Value   `json:"after,omitempty"`
	BeforeSources []string `json:"beforeSources"`
	AfterSources  []string `json:"afterSources"`
}
type PossibleRename struct {
	Element  string   `json:"element"`
	Before   string   `json:"before"`
	After    string   `json:"after"`
	Evidence []string `json:"evidence"`
}
type Report struct {
	SemanticChanges   []Change         `json:"semanticChanges"`
	ProvenanceChanges []Change         `json:"provenanceChanges"`
	PossibleRenames   []PossibleRename `json:"possibleRenames"`
}
type diffElement struct {
	kind    string
	value   Value
	sources []string
}

// Diff compares resolved graphs without executing implementations or modifying inputs.
// Place and arc provenance requires DiffResults; transition Sources are available here.
func Diff(before, after meshwork.ExecutableGraph) (Report, error) {
	return DiffResults(Result{Graph: before}, Result{Graph: after})
}

func diffIndex(r Result) (map[string]diffElement, error) {
	if r.Graph.Stage != "" && r.Graph.Stage != "linked" && r.Graph.Stage != "flattened" {
		return nil, fmt.Errorf("unknown graph resolution stage %q", r.Graph.Stage)
	}
	if len(r.Graph.References) > 0 && r.Graph.Stage != "linked" {
		return nil, fmt.Errorf("graph references require linked resolution stage")
	}
	index := map[string]diffElement{}
	valid := func(n string) bool {
		resolved, err := meshwork.Resolve("/", n)
		return err == nil && resolved == n && n != "/"
	}
	add := func(id, kind string, v Value, fallback []string) error {
		if _, exists := index[id]; exists {
			return fmt.Errorf("duplicate computational identity %s", id)
		}
		sources := append([]string{}, r.Sources[id]...)
		if len(sources) == 0 {
			sources = append(sources, fallback...)
		}
		sort.Strings(sources)
		index[id] = diffElement{kind, v, dedup(sources)}
		return nil
	}
	for _, p := range r.Graph.Places {
		if !valid(p.Name) || strings.ContainsRune(p.Type, 0) {
			return nil, fmt.Errorf("invalid place name/type %q", p.Name)
		}
		if err := add(p.Name, "place", Value{Type: p.Type}, nil); err != nil {
			return nil, err
		}
	}
	for _, t := range r.Graph.Transitions {
		if !valid(t.Name) || t.Timeout < 0 {
			return nil, fmt.Errorf("invalid transition %q or timeout", t.Name)
		}
		if err := add(t.Name, "transition", Value{Implementation: t.Implementation, Timeout: t.Timeout}, t.Sources); err != nil {
			return nil, err
		}
	}
	for _, t := range r.Graph.Transitions {
		if err := t.ValidateTransports(); err != nil {
			return nil, err
		}
		for direction, endpoints := range [][]string{t.Inputs, t.Outputs} {
			ports := t.InputPorts
			if direction == 1 {
				ports = t.OutputPorts
			}
			if len(ports) != 0 && len(ports) != len(endpoints) {
				return nil, fmt.Errorf("%s: port count does not match endpoints", t.Name)
			}
			seenPorts := map[string]bool{}
			for i, p := range endpoints {
				endpoint, ok := index[p]
				if !ok || endpoint.kind != "place" {
					return nil, fmt.Errorf("%s: invalid bipartite arc endpoint %s (expected declared place)", t.Name, p)
				}
				local := path.Base(p)
				if len(ports) > 0 {
					local = ports[i]
				}
				if local == "" || seenPorts[local] {
					return nil, fmt.Errorf("%s: invalid or duplicate local port %q", t.Name, local)
				}
				seenPorts[local] = true
				from, to := p, t.Name
				if direction == 1 {
					from, to = t.Name, p
				}
				if err := add(from+" -> "+to, "arc", Value{From: from, To: to, Port: local, Transport: arcTransport(t, direction, local)}, t.Sources); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, ref := range r.Graph.References {
		if !valid(ref.Graph) || (ref.Namespace != "/" && !valid(ref.Namespace)) {
			return nil, fmt.Errorf("unresolved graph reference")
		}
		bindings := map[string]string{}
		for k, v := range ref.Bindings {
			if !valid(k) || !valid(v) {
				return nil, fmt.Errorf("unresolved reference binding")
			}
			bindings[k] = v
		}
		if err := add(ref.Namespace+" uses "+ref.Graph, "reference", Value{Graph: ref.Graph, Namespace: ref.Namespace, Bindings: bindings}, nil); err != nil {
			return nil, err
		}
	}
	return index, nil
}

// DiffResults compares only Graph semantics; Sources is independent provenance.
// Imports, definitions, diagnostics, scenarios and presentation are not computation.
func DiffResults(before, after Result) (Report, error) {
	stage := func(g meshwork.Graph) string {
		if g.Stage == "" {
			return "flattened"
		}
		return g.Stage
	}
	if stage(before.Graph) != stage(after.Graph) {
		return Report{}, fmt.Errorf("compare graphs at the same resolution stage (linked or flattened)")
	}
	report := Report{SemanticChanges: []Change{}, ProvenanceChanges: []Change{}, PossibleRenames: []PossibleRename{}}
	a, err := diffIndex(before)
	if err != nil {
		return Report{}, fmt.Errorf("before graph: %w", err)
	}
	b, err := diffIndex(after)
	if err != nil {
		return Report{}, fmt.Errorf("after graph: %w", err)
	}
	ids := map[string]bool{}
	for id := range a {
		ids[id] = true
	}
	for id := range b {
		ids[id] = true
	}
	for _, id := range sortedKeys(ids) {
		old, hasOld := a[id]
		next, hasNext := b[id]
		c := Change{Identity: id, Element: old.kind, BeforeSources: old.sources, AfterSources: next.sources}
		if c.BeforeSources == nil {
			c.BeforeSources = []string{}
		}
		if c.AfterSources == nil {
			c.AfterSources = []string{}
		}
		if hasOld {
			v := old.value
			c.Before = &v
		}
		if hasNext {
			v := next.value
			c.After = &v
			c.Element = next.kind
		}
		switch {
		case !hasOld:
			c.Kind = next.kind + "_added"
		case !hasNext:
			c.Kind = old.kind + "_removed"
		case old.kind != next.kind:
			removed := c
			removed.Kind = old.kind + "_removed"
			removed.Element = old.kind
			removed.After = nil
			removed.AfterSources = []string{}
			report.SemanticChanges = append(report.SemanticChanges, removed)
			c.Kind = next.kind + "_added"
			c.Before = nil
			c.BeforeSources = []string{}
		case !reflect.DeepEqual(old.value, next.value):
			switch old.kind {
			case "place":
				c.Kind = "place_type_changed"
			case "transition":
				c.Kind = "transition_definition_changed"
			case "reference":
				c.Kind = "reference_binding_changed"
			case "arc":
				removed := c
				removed.Kind = "arc_removed"
				removed.After = nil
				removed.AfterSources = []string{}
				report.SemanticChanges = append(report.SemanticChanges, removed)
				c.Kind = "arc_added"
				c.Before = nil
				c.BeforeSources = []string{}
			}
		}
		if c.Kind != "" {
			report.SemanticChanges = append(report.SemanticChanges, c)
		}
		if hasOld && hasNext && old.kind == next.kind && strings.Join(old.sources, "\x00") != strings.Join(next.sources, "\x00") {
			report.ProvenanceChanges = append(report.ProvenanceChanges, Change{Kind: "provenance_changed", Element: old.kind, Identity: id, BeforeSources: old.sources, AfterSources: next.sources})
		}
	}
	// Index all node source anchors so reused IDs or one-to-many bindings are ambiguous.
	anchors := func(index map[string]diffElement) map[string][]string {
		out := map[string][]string{}
		for id, e := range index {
			if e.kind == "arc" {
				continue
			}
			for _, s := range e.sources {
				if file, node, ok := strings.Cut(s, "#"); ok && node != "" && strings.HasSuffix(strings.ToLower(file), ".graphml") {
					out[s] = append(out[s], id)
				}
			}
		}
		return out
	}
	aa, bb := anchors(a), anchors(b)
	candidates := map[string]map[string][]string{}
	reverse := map[string]map[string]bool{}
	for _, anchor := range sortedKeys(aa) {
		old, next := aa[anchor], bb[anchor]
		if len(old) != 1 || len(next) != 1 || old[0] == next[0] {
			continue
		}
		x, y := old[0], next[0]
		if _, ok := b[x]; ok {
			continue
		}
		if _, ok := a[y]; ok {
			continue
		}
		if a[x].kind != b[y].kind {
			continue
		}
		if candidates[x] == nil {
			candidates[x] = map[string][]string{}
		}
		candidates[x][y] = append(candidates[x][y], anchor)
		if reverse[y] == nil {
			reverse[y] = map[string]bool{}
		}
		reverse[y][x] = true
	}
	for _, x := range sortedKeys(candidates) {
		if len(candidates[x]) != 1 {
			continue
		}
		for y, evidence := range candidates[x] {
			if len(reverse[y]) == 1 {
				report.PossibleRenames = append(report.PossibleRenames, PossibleRename{a[x].kind, x, y, evidence})
			}
		}
	}
	return report, nil
}

// WriteText renders all semantic values and source references, including provenance-only edits.
func (r Report) WriteText(w io.Writer, before, after string) error {
	var s strings.Builder
	fmt.Fprintf(&s, "Meshwork Semantic Diff\n\nBefore: %s\nAfter:  %s\n\n", before, after)
	if len(r.SemanticChanges) == 0 {
		s.WriteString("No semantic changes.\nPresentation differences are ignored.\n")
	} else {
		s.WriteString("Semantic changes:\n")
		for _, c := range r.SemanticChanges {
			fmt.Fprintf(&s, "  %s %s\n", c.Kind, c.Identity)
			if c.Before != nil {
				b, _ := json.Marshal(c.Before)
				fmt.Fprintf(&s, "    before: %s\n", b)
			}
			if c.After != nil {
				b, _ := json.Marshal(c.After)
				fmt.Fprintf(&s, "    after: %s\n", b)
			}
			fmt.Fprintf(&s, "    sources before: %v\n    sources after: %v\n", c.BeforeSources, c.AfterSources)
		}
	}
	s.WriteString("\nProvenance changes:\n")
	if len(r.ProvenanceChanges) == 0 {
		s.WriteString("  None\n")
	}
	for _, c := range r.ProvenanceChanges {
		fmt.Fprintf(&s, "  %s\n    %v -> %v\n", c.Identity, c.BeforeSources, c.AfterSources)
	}
	s.WriteString("\nPossible renames:\n")
	if len(r.PossibleRenames) == 0 {
		s.WriteString("  None\n")
	}
	for _, c := range r.PossibleRenames {
		fmt.Fprintf(&s, "  %s -> %s\n    Same GraphML source node: %v\n", c.Before, c.After, c.Evidence)
	}
	_, err := io.WriteString(w, s.String())
	return err
}

func arcTransport(t meshwork.AtomicTransition, direction int, p string) string {
	bindings := t.InputTransports
	if direction == 1 {
		bindings = t.OutputTransports
	}
	if bindings[p] == "memory" {
		return ""
	}
	return bindings[p]
}
