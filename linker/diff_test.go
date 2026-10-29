package linker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"example.com/meshwork-prototype/meshwork"
)

func cloneResult(t *testing.T, r Result) Result {
	t.Helper()
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var c Result
	if e = json.Unmarshal(b, &c); e != nil {
		t.Fatal(e)
	}
	return c
}
func compare(t *testing.T, a, b Result) Report {
	t.Helper()
	r, e := DiffResults(a, b)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func kinds(r Report) []string {
	out := []string{}
	for _, c := range r.SemanticChanges {
		out = append(out, c.Kind)
	}
	return out
}
func nandResult(t *testing.T) Result {
	t.Helper()
	r, e := ImportGraphML("testdata/graphml/graph.nand.graphml", false)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestDiffSemanticChanges(t *testing.T) {
	base := nandResult(t)
	tests := []struct {
		name string
		edit func(*Result)
		want []string
	}{
		{"identical", func(r *Result) {}, []string{}},
		{"place added", func(r *Result) { r.Graph.Places = append(r.Graph.Places, meshwork.Place{Name: "/extra", Type: "Bool"}) }, []string{"place_added"}},
		{"place removed and arcs", func(r *Result) {
			r.Graph.Places = r.Graph.Places[1:]
			r.Graph.Transitions[0].Inputs = r.Graph.Transitions[0].Inputs[1:]
			r.Graph.Transitions[0].InputPorts = r.Graph.Transitions[0].InputPorts[1:]
		}, []string{"place_removed", "arc_removed"}},
		{"place type", func(r *Result) { r.Graph.Places[0].Type = "Bit" }, []string{"place_type_changed"}},
		{"transition added", func(r *Result) {
			r.Graph.Transitions = append(r.Graph.Transitions, meshwork.AtomicTransition{Name: "/extra"})
		}, []string{"transition_added"}},
		{"transition removed", func(r *Result) { r.Graph.Transitions = nil }, []string{"arc_removed", "arc_removed", "transition_removed", "arc_removed"}},
		{"implementation", func(r *Result) { r.Graph.Transitions[0].Implementation = "other" }, []string{"transition_definition_changed"}},
		{"timeout", func(r *Result) { r.Graph.Transitions[0].Timeout = 1 }, []string{"transition_definition_changed"}},
		{"arc added", func(r *Result) {
			r.Graph.Transitions[0].Inputs = append(r.Graph.Transitions[0].Inputs, "/logic/nand/Q")
			r.Graph.Transitions[0].InputPorts = append(r.Graph.Transitions[0].InputPorts, "Q")
		}, []string{"arc_added"}},
		{"arc removed", func(r *Result) {
			r.Graph.Transitions[0].Inputs = r.Graph.Transitions[0].Inputs[1:]
			r.Graph.Transitions[0].InputPorts = r.Graph.Transitions[0].InputPorts[1:]
		}, []string{"arc_removed"}},
		{"port changed", func(r *Result) { r.Graph.Transitions[0].InputPorts[0] = "renamed" }, []string{"arc_removed", "arc_added"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := cloneResult(t, base)
			tt.edit(&next)
			r := compare(t, base, next)
			if !reflect.DeepEqual(kinds(r), tt.want) {
				t.Fatalf("%v != %v", kinds(r), tt.want)
			}
			if tt.name == "place type" && (r.SemanticChanges[0].Before.Type != "Bool" || r.SemanticChanges[0].After.Type != "Bit") {
				t.Fatal(r)
			}
		})
	}
}

func TestDiffGraphMLPresentationAndRename(t *testing.T) {
	text := graphMLFixture(t)
	// Use one physical source across versions, preserving document-local anchors.
	filename := filepath.Join(t.TempDir(), "graph.nand.graphml")
	load := func(s string) Result {
		t.Helper()
		if e := os.WriteFile(filename, []byte(s), 0644); e != nil {
			t.Fatal(e)
		}
		r, e := ImportGraphML(filename, false)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	base := load(text)
	nodeStart := strings.Index(text, `<node id="n0">`)
	nodeEnd := nodeStart + strings.Index(text[nodeStart:], "</node>") + len("</node>")
	node := text[nodeStart:nodeEnd]
	// Move A after B, rather than changing its declaration or source ID.
	bStart := strings.Index(text, `<node id="n1">`)
	bEnd := bStart + strings.Index(text[bStart:], "</node>") + len("</node>")
	reordered := strings.Replace(strings.Replace(text, node, "", 1), text[bStart:bEnd], text[bStart:bEnd]+node, 1)
	for name, variant := range map[string]string{
		"node order": reordered,
		"edge order": strings.Replace(text, `<edge id="e1" source="n1" target="n2"/>`+"\n    "+`<edge id="e2" source="n2" target="n3"/>`, `<edge id="e2" source="n2" target="n3"/>`+"\n    "+`<edge id="e1" source="n1" target="n2"/>`, 1),
		"move":       strings.ReplaceAll(text, `x="10.0"`, `x="900.0"`),
		"color":      strings.ReplaceAll(text, "#FFCC00", "#ABCDEF"),
		"order":      strings.ReplaceAll(text, `<data key="kind">place</data><data key="name">A</data>`, `<data key="name">A</data><data key="kind">place</data>`),
		"group":      strings.ReplaceAll(text, `<node id="nested" yfiles.foldertype="group"><graph id="nested:" edgedefault="directed">`, ""),
		"node ids":   strings.ReplaceAll(text, "n0", "new-id"),
	} {
		t.Run(name, func(t *testing.T) {
			if variant == text {
				t.Fatal("fixture edit did not apply")
			}
			if name == "group" {
				variant = strings.Replace(variant, "</graph></node>", "", 1)
			}
			r := compare(t, base, load(variant))
			if len(r.SemanticChanges) != 0 {
				t.Fatal(r)
			}
			if name == "node ids" && len(r.ProvenanceChanges) == 0 {
				t.Fatal("missing source ID change")
			}
		})
	}
	renamed := load(strings.Replace(text, `<data key="name">Q</data>`, `<data key="name">result</data>`, 1))
	r := compare(t, base, renamed)
	if len(r.PossibleRenames) != 1 || r.PossibleRenames[0].Before != "/logic/nand/Q" || r.PossibleRenames[0].After != "/logic/nand/result" {
		t.Fatal(r)
	}
	found := map[string]bool{}
	for _, c := range r.SemanticChanges {
		found[c.Kind] = true
		if len(c.BeforeSources)+len(c.AfterSources) == 0 {
			t.Fatal("lost provenance", c)
		}
	}
	if !found["place_added"] || !found["place_removed"] || !found["arc_added"] || !found["arc_removed"] {
		t.Fatal(r)
	}
	var output bytes.Buffer
	if e := r.WriteText(&output, "before", "after"); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(output.String(), "Same GraphML source node") {
		t.Fatal(output.String())
	}
	// A regenerated ID has no safe rename evidence.
	noAnchor := load(strings.ReplaceAll(strings.Replace(text, `<data key="name">Q</data>`, `<data key="name">result</data>`, 1), "n3", "regenerated"))
	if len(compare(t, base, noAnchor).PossibleRenames) != 0 {
		t.Fatal("guessed rename")
	}
}

func TestDiffAmbiguousRenames(t *testing.T) {
	a := Result{Graph: meshwork.ExecutableGraph{Places: []meshwork.Place{{Name: "/a", Type: "Bool"}}}, Sources: map[string][]string{"/a": {"same.graphml#n0", "same.graphml#n1"}}}
	b := Result{Graph: meshwork.ExecutableGraph{Places: []meshwork.Place{{Name: "/b", Type: "Bool"}, {Name: "/c", Type: "Bool"}}}, Sources: map[string][]string{"/b": {"same.graphml#n0"}, "/c": {"same.graphml#n1"}}}
	if len(compare(t, a, b).PossibleRenames) != 0 {
		t.Fatal("ambiguous one-to-many suggestion")
	}
	if len(compare(t, b, a).PossibleRenames) != 0 {
		t.Fatal("ambiguous many-to-one suggestion")
	}
	b.Sources["/c"] = []string{"same.graphml#n0"}
	if len(compare(t, a, b).PossibleRenames) != 0 {
		t.Fatal("shared ID suggestion")
	}
}

func TestDiffYAMLProvenanceAndDiscovery(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "graph.a.meshwork.yaml")
	b := filepath.Join(root, "graph.b.meshwork.yaml")
	write := func(p, s string) {
		t.Helper()
		if e := os.WriteFile(p, []byte(s), 0644); e != nil {
			t.Fatal(e)
		}
	}
	load := func() Result {
		t.Helper()
		r, e := LoadComparison(root, false)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	write(a, "namespace: /g\nplaces: {A: Bool, B: Bool}\ntransitions: {t: {}}\narcs: [[A,t],[t,B]]\n")
	base := load()
	write(a, "namespace: /g\nplaces: {B: Bool, A: Bool}\ntransitions: {t: {}}\narcs: [[t,B],[A,t]]\n")
	if r := compare(t, base, load()); len(r.SemanticChanges) != 0 || len(r.ProvenanceChanges) != 0 {
		t.Fatal(r)
	}
	write(a, "namespace: /g\nplaces: {B: Bool}\ntransitions: {t: {}}\narcs: [[t,B],[A,t]]\n")
	write(b, "namespace: /g\nplaces: {A: Bool}\n")
	moved := load()
	r := compare(t, base, moved)
	if len(r.SemanticChanges) != 0 || len(r.ProvenanceChanges) != 1 {
		t.Fatal(r)
	}
	write(b, "namespace: /g\nplaces: {A: Bool, B: Bool}\n")
	joint := load()
	if r := compare(t, moved, joint); len(r.SemanticChanges) != 0 || len(r.ProvenanceChanges) != 1 {
		t.Fatal(r)
	}
	files, e := Discover([]Repository{{Name: "workspace", Root: root}})
	if e != nil {
		t.Fatal(e)
	}
	expected, _ := json.Marshal(compare(t, base, joint))
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 20; i++ {
		rng.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })
		got, e := linkFiles(files, true)
		if e != nil {
			t.Fatal(e)
		}
		actual, _ := json.Marshal(compare(t, base, got))
		if !bytes.Equal(expected, actual) {
			t.Fatal("discovery order affected report")
		}
	}
	// Plain YAML files and incomplete declarations are supported by the comparison loader.
	direct, e := LoadComparison(b, false)
	if e != nil || len(direct.Graph.Places) != 2 {
		t.Fatal(direct, e)
	}
}

func TestDiffValidation(t *testing.T) {
	base := nandResult(t)
	for name, edit := range map[string]func(*Result){
		"duplicate place":     func(r *Result) { r.Graph.Places = append(r.Graph.Places, r.Graph.Places[0]) },
		"kind collision":      func(r *Result) { r.Graph.Transitions[0].Name = r.Graph.Places[0].Name },
		"relative identity":   func(r *Result) { r.Graph.Places[0].Name = "relative" },
		"malformed identity":  func(r *Result) { r.Graph.Places[0].Name = "/bad//path" },
		"invalid type":        func(r *Result) { r.Graph.Places[0].Type = "bad\x00type" },
		"non bipartite":       func(r *Result) { r.Graph.Transitions[0].Inputs[0] = r.Graph.Transitions[0].Name },
		"undeclared endpoint": func(r *Result) { r.Graph.Transitions[0].Outputs[0] = "/absent" },
		"port mismatch":       func(r *Result) { r.Graph.Transitions[0].InputPorts = []string{"one"} },
		"duplicate port":      func(r *Result) { r.Graph.Transitions[0].InputPorts = []string{"same", "same"} },
		"negative timeout":    func(r *Result) { r.Graph.Transitions[0].Timeout = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := cloneResult(t, base)
			edit(&bad)
			for _, pair := range [][2]Result{{bad, base}, {base, bad}} {
				r, e := DiffResults(pair[0], pair[1])
				if e == nil || len(r.SemanticChanges) > 0 {
					t.Fatal("accepted invalid graph", e, r)
				}
			}
		})
	}
	incomplete := cloneResult(t, base)
	incomplete.Graph.Places[0].Type = ""
	incomplete.Graph.Transitions[0].Implementation = ""
	if _, e := Diff(base.Graph, incomplete.Graph); e != nil {
		t.Fatal(e)
	}
}

func TestDiffDeterminismPurityAndScale(t *testing.T) {
	a := Result{Sources: map[string][]string{}}
	for i := 0; i < 3000; i++ {
		name := fmt.Sprintf("/p%05d", i)
		a.Graph.Places = append(a.Graph.Places, meshwork.Place{Name: name, Type: "Bool"})
		a.Sources[name] = []string{"z.yaml:2", "a.yaml:1", "z.yaml:2"}
		if i > 0 {
			a.Graph.Transitions = append(a.Graph.Transitions, meshwork.AtomicTransition{Name: fmt.Sprintf("/t%05d", i), Inputs: []string{fmt.Sprintf("/p%05d", i-1)}, Outputs: []string{name}})
		}
	}
	b := cloneResult(t, a)
	b.Graph.Places[0].Type = "Bit"
	b.Graph.Transitions[0].Implementation = "new"
	originalA, _ := json.Marshal(a)
	originalB, _ := json.Marshal(b)
	expected, _ := json.Marshal(compare(t, a, b))
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 5; i++ {
		shuffled := cloneResult(t, b)
		rng.Shuffle(len(shuffled.Graph.Places), func(i, j int) {
			shuffled.Graph.Places[i], shuffled.Graph.Places[j] = shuffled.Graph.Places[j], shuffled.Graph.Places[i]
		})
		rng.Shuffle(len(shuffled.Graph.Transitions), func(i, j int) {
			shuffled.Graph.Transitions[i], shuffled.Graph.Transitions[j] = shuffled.Graph.Transitions[j], shuffled.Graph.Transitions[i]
		})
		got, _ := json.Marshal(compare(t, a, shuffled))
		if !bytes.Equal(expected, got) {
			t.Fatal("unstable JSON")
		}
	}
	nowA, _ := json.Marshal(a)
	nowB, _ := json.Marshal(b)
	if !bytes.Equal(originalA, nowA) || !bytes.Equal(originalB, nowB) {
		t.Fatal("inputs mutated")
	}
}

func TestDiffLinkedReuseAndSourceMultiplicity(t *testing.T) {
	imported := nandResult(t)
	arcSources := imported.Sources["/logic/nand/A -> /logic/nand/nand"]
	if len(arcSources) != 1 || !strings.HasSuffix(arcSources[0], ".graphml#e0") {
		t.Fatal("arc source must identify the edge, not a visual group", arcSources)
	}
	repos := []Repository{{Name: "primitives", Root: "../examples/distributed/primitives"}, {Name: "composites", Root: "../examples/distributed/composites"}}
	files, e := Discover(repos)
	if e != nil {
		t.Fatal(e)
	}
	first, e := LinkFiles(files)
	if e != nil {
		t.Fatal(e)
	}
	for i, j := 0, len(files)-1; i < j; i, j = i+1, j-1 {
		files[i], files[j] = files[j], files[i]
	}
	second, e := LinkFiles(files)
	if e != nil {
		t.Fatal(e)
	}
	if r := compare(t, first, second); len(r.SemanticChanges)+len(r.ProvenanceChanges) != 0 {
		t.Fatal(r)
	}
	mux := first.Definitions["/logic/mux"]
	if len(mux.Transitions) == 0 {
		t.Fatal("MUX not exercised")
	}
	for _, p := range mux.Places {
		if len(first.Sources[p.Name]) == 0 {
			t.Fatal("lost remapped place source", p.Name)
		}
	}
	for _, tr := range mux.Transitions {
		for _, p := range tr.Inputs {
			if len(first.Sources[p+" -> "+tr.Name]) == 0 {
				t.Fatal("lost remapped arc source", p, tr.Name)
			}
		}
	}
	text := graphMLFixture(t)
	extra := `<node id="another-A"><data key="kind">place</data><data key="name">A</data><data key="type">Bool</data></node>`
	duplicate, e := importText(t, strings.Replace(text, `<node id="n3">`, extra+`<node id="n3">`, 1), false)
	if e != nil {
		t.Fatal(e)
	}
	if len(duplicate.Sources["/logic/nand/A"]) != 2 {
		t.Fatal("merged GraphML declaration lost source", duplicate.Sources)
	}
}
