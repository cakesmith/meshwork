package linker

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"example.com/meshwork-prototype/meshwork"
)

func graphMLFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/graphml/graph.nand.graphml")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func importText(t *testing.T, text string, yed bool) (Result, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "graph.nand.graphml")
	if err := os.WriteFile(p, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	return ImportGraphML(p, yed)
}
func semanticBytes(t *testing.T, r Result) []byte {
	t.Helper()
	b, err := CanonicalJSON(r.Graph)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestGraphMLNANDEquivalence(t *testing.T) {
	text := graphMLFixture(t)
	xml, err := importText(t, text, false)
	if err != nil {
		t.Fatal(err)
	}
	yaml, err := Link([]Repository{{Name: "test", Root: "testdata/graphml"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(semanticBytes(t, xml), semanticBytes(t, yaml)) {
		t.Fatalf("not equivalent: %+v vs %+v", xml.Graph, yaml.Graph)
	}
	if len(xml.Diagnostics) != 0 || len(xml.Graph.Places) != 3 || len(xml.Graph.Transitions) != 1 {
		t.Fatalf("bad import %+v", xml)
	}
	for name, variant := range map[string]string{
		"geometry":                  strings.ReplaceAll(text, `x="10.0"`, `x="999.0"`),
		"color":                     strings.ReplaceAll(text, "#FFCC00", "#0000FF"),
		"ids":                       strings.NewReplacer("n0", "document-local-99", "n2", "another-id").Replace(text),
		"explicit kind beats shape": strings.ReplaceAll(text, `type="ellipse"`, `type="rectangle"`),
		"edge order":                strings.Replace(text, `<edge id="e1" source="n1" target="n2"/>`+"\n    "+`<edge id="e2" source="n2" target="n3"/>`, `<edge id="e2" source="n2" target="n3"/>`+"\n    "+`<edge id="e1" source="n1" target="n2"/>`, 1),
		"data order":                strings.ReplaceAll(text, `<data key="kind">place</data><data key="name">A</data>`, `<data key="name">A</data><data key="kind">place</data>`),
	} {
		t.Run(name, func(t *testing.T) {
			r, err := importText(t, variant, false)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(semanticBytes(t, r), semanticBytes(t, xml)) {
				t.Fatal("presentation/order/ID changed computation")
			}
		})
	}
	changed, err := importText(t, strings.Replace(text, ">Bool<", ">Other<", 1), false)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(semanticBytes(t, changed), semanticBytes(t, xml)) {
		t.Fatal("type ignored")
	}
	prov := xml.Imports[0]
	if prov.XML != text || len(prov.Provenance) != 10 {
		t.Fatalf("lost source: %+v", prov.Provenance)
	}
	if node := prov.Nodes["n0"]; node.Label != "A" || node.Shape != "ellipse" || node.Geometry == nil || node.Geometry.X != "10.0" || !prov.Nodes["nested"].Group {
		t.Fatal("lost structured yEd presentation", prov.Nodes)
	}
	found := false
	for _, p := range prov.Provenance {
		if p.ID == "n0" {
			found = p.Identity == "/logic/nand/A" && p.Properties["meshwork.type"] == "Bool"
		}
	}
	if !found || !strings.Contains(xml.Graph.Transitions[0].Sources[0], "#n2") {
		t.Fatal("lost identity provenance")
	}
	export, err := CanonicalYAML(xml.Graph)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	write(t, root, "graph.export.meshwork.yaml", string(export))
	round, err := Link(repo(root))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(semanticBytes(t, round), semanticBytes(t, xml)) {
		t.Fatal("YAML round trip changed semantics")
	}
	// Execute through the existing registry/kernel, without an importer execution path.
	_, registry := meshwork.LogicLibrary()
	registry["builtin.nand"] = registry["logic.nand.go"]
	for _, a := range []bool{false, true} {
		for _, b := range []bool{false, true} {
			rt, err := xml.Graph.Instantiate(registry)
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]bool{"A": a, "B": b} {
				token, _ := meshwork.NewToken("Bool", value)
				if err := rt.Put("/logic/nand/"+name, token); err != nil {
					t.Fatal(err)
				}
			}
			if err := rt.Fire(context.Background(), "/logic/nand/nand"); err != nil {
				t.Fatal(err)
			}
			token, ok := rt.Token("/logic/nand/Q")
			var q bool
			if !ok || token.Decode(&q) != nil || q != !(a && b) {
				t.Fatal(rt.Snapshot())
			}
		}
	}
}
func TestGraphMLRejectsInvalidInput(t *testing.T) {
	text := graphMLFixture(t)
	cases := map[string]string{
		"place arc":           strings.Replace(text, `source="n0" target="n2"`, `source="n0" target="n1"`, 1),
		"transition arc":      strings.Replace(text, `source="n0" target="n2"`, `source="n2" target="n2"`, 1),
		"bad reference":       strings.Replace(text, `source="n0"`, `source="missing"`, 1),
		"edge reference":      strings.Replace(text, `source="n1"`, `source="e0"`, 1),
		"undirected":          strings.Replace(text, `id="e0"`, `id="e0" directed="false"`, 1),
		"malformed":           text[:len(text)/2],
		"DOCTYPE":             strings.Replace(text, "<graphml ", `<!DOCTYPE graphml SYSTEM "file:///secrets">`+"<graphml ", 1),
		"unknown entity":      strings.Replace(text, ">A<", ">&external;<", 1),
		"duplicate node":      strings.Replace(text, `id="n1"`, `id="n0"`, 1),
		"unknown key":         strings.Replace(text, `key="type"`, `key="missing"`, 1),
		"typo property":       strings.ReplaceAll(text, "meshwork.type", "meshwork.typo"),
		"bad kind":            strings.Replace(text, ">place<", ">plac<", 1),
		"computational group": strings.Replace(text, `<node id="group" yfiles.foldertype="group">`, `<node id="group" yfiles.foldertype="group"><data key="kind">place</data>`, 1),
		"annotation endpoint": strings.Replace(text, `source="n0"`, `source="legend"`, 1),
		"conflicting names":   strings.Replace(text, `<data key="name">nand</data>`, `<data key="name">A</data>`, 1),
		"trailing document":   text + text,
		"oversize":            strings.Repeat(" ", MaxGraphMLBytes+1),
		"depth":               `<graphml xmlns="http://graphml.graphdrawing.org/xmlns">` + strings.Repeat("<node>", 129) + strings.Repeat("</node>", 129) + "</graphml>",
	}
	for name, variant := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := importText(t, variant, false)
			if err == nil || !reflect.DeepEqual(r, Result{}) {
				t.Fatalf("accepted invalid input: %v", err)
			}
			if !strings.Contains(err.Error(), "graph.nand.graphml") {
				t.Fatal("no source filename", err)
			}
		})
	}
}
func TestGraphMLIncompleteAndConventions(t *testing.T) {
	text := graphMLFixture(t)
	partial := strings.ReplaceAll(text, `<data key="type">Bool</data>`, "")
	partial = strings.Replace(partial, `<data key="impl">builtin.nand</data>`, "", 1)
	r, err := importText(t, partial, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 4 {
		t.Fatal(r.Diagnostics)
	}
	if _, err := r.Graph.Instantiate(nil); err == nil {
		t.Fatal("incomplete graph executable")
	}
	// Unknown implementation cannot run even when an empty-key registry entry exists.
	knownTypes := strings.Replace(text, `<data key="impl">builtin.nand</data>`, "", 1)
	r, err = importText(t, knownTypes, false)
	if err != nil {
		t.Fatal(err)
	}
	_, registry := meshwork.LogicLibrary()
	registry[""] = registry["logic.nand.go"]
	if _, err := r.Graph.Instantiate(registry); err == nil {
		t.Fatal("invented missing implementation")
	}
	inferred := strings.ReplaceAll(partial, `<data key="kind">place</data>`, "")
	inferred = strings.ReplaceAll(inferred, `<data key="kind">transition</data>`, "")
	inferred = strings.ReplaceAll(inferred, `<data key="name">nand</data>`, "")
	inferred = strings.ReplaceAll(inferred, "<y:NodeLabel>NAND</y:NodeLabel>", "<y:NodeLabel>nand</y:NodeLabel>")
	r, err = importText(t, inferred, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Graph.Places) != 3 || len(r.Diagnostics) != 4 {
		t.Fatal(r)
	}
	for _, p := range r.Imports[0].Provenance {
		if p.Identity != "" && !strings.Contains(p.Identity, " -> ") && p.Convention != "yed-shape-label" {
			t.Fatal(p)
		}
	}
	// Unspecified nodes stay decorative in explicit mode; arcs are removed here too.
	noEdges := inferred[:strings.Index(inferred, `    <edge id="e0"`)] + "</graph></graphml>"
	r, err = importText(t, noEdges, false)
	if err != nil || len(r.Graph.Places) != 0 || len(r.Graph.Transitions) != 0 {
		t.Fatalf("guessed computational node: %+v %v", r, err)
	}
	architecture := strings.Replace(text, `<key id="impl" for="node" attr.name="meshwork.implementation" attr.type="string"/>`, `<key id="impl" for="node" attr.name="meshwork.architecture" attr.type="string"/>`, 1)
	r, err = importText(t, architecture, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Graph.Transitions[0].Architecture != "builtin.nand" || len(r.Diagnostics) != 2 {
		t.Fatal(r.Diagnostics)
	}
	if _, err := r.Graph.Instantiate(nil); err == nil {
		t.Fatal("unresolved architecture executed")
	}
}
func TestGraphMLDiscoveryAndMerging(t *testing.T) {
	root := t.TempDir()
	text := graphMLFixture(t)
	write(t, root, "graph.nand.graphml", text)
	files, err := Discover(repo(root))
	if err != nil || len(files) != 0 {
		t.Fatal(files, err)
	}
	files, err = DiscoverGraphML(repo(root), false)
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	one, err := LinkFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "other.graphml", strings.NewReplacer("n0", "x0", "n1", "x1", "n2", "x2", "n3", "x3").Replace(text))
	b, err := os.ReadFile("testdata/graphml/graph.nand.meshwork.yaml")
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "graph.nand.meshwork.yaml", string(b))
	files, err = DiscoverGraphML(repo(root), false)
	if err != nil {
		t.Fatal(err)
	}
	yaml, err := Discover(repo(root))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, yaml...)
	merged, err := LinkFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(semanticBytes(t, one), semanticBytes(t, merged)) {
		t.Fatal("merge changed computation")
	}
	if len(merged.Graph.Transitions[0].Sources) != 3 || len(merged.Imports) != 2 {
		t.Fatal("lost contributing sources")
	}
	reversed := append([]File(nil), files...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	again, err := LinkFiles(reversed)
	if err != nil || !reflect.DeepEqual(merged, again) {
		t.Fatal("nondeterministic linking", err)
	}
	write(t, root, "other.graphml", strings.Replace(text, ">Bool<", ">Image<", 1))
	for _, order := range [][]File{files, reversed} {
		r, err := LinkFiles(order)
		if err == nil || !reflect.DeepEqual(r, Result{}) || !strings.Contains(err.Error(), "#n0") {
			t.Fatal("conflict not rejected with provenance", err)
		}
	}
}

func TestGraphMLComplementaryContributions(t *testing.T) {
	text := graphMLFixture(t)
	root := t.TempDir()
	a := strings.Replace(text, `<data key="impl">builtin.nand</data>`, "", 1)
	a = strings.Replace(a, `<edge id="e2" source="n2" target="n3"/>`, "", 1)
	b := strings.ReplaceAll(text, `<data key="type">Bool</data>`, "")
	start, end := strings.Index(b, `    <edge id="e0"`), strings.Index(b, `    <edge id="e2"`)
	b = b[:start] + b[end:]
	write(t, root, "a.graphml", a)
	write(t, root, "b.graphml", b)
	files, err := DiscoverGraphML(repo(root), false)
	if err != nil {
		t.Fatal(err)
	}
	r, err := LinkFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	full, err := importText(t, text, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 0 || !bytes.Equal(semanticBytes(t, r), semanticBytes(t, full)) {
		t.Fatal("split graph differs", r.Diagnostics)
	}
	// Explicit node namespaces may span visual groups and differ from the graph namespace.
	different := strings.Replace(text, `<key id="type"`, `<key id="nodeNS" for="node" attr.name="meshwork.namespace" attr.type="string"/><key id="type"`, 1)
	different = strings.Replace(different, `<data key="name">A</data>`, `<data key="name">A</data><data key="nodeNS">/another</data>`, 1)
	r, err = importText(t, different, false)
	if err != nil || r.Graph.Places[0].Name != "/another/A" {
		t.Fatal("group overrode explicit namespace", err)
	}
	// Key defaults provide semantic data through standard GraphML rules.
	defaulted := strings.ReplaceAll(text, `<data key="type">Bool</data>`, "")
	defaulted = strings.Replace(defaulted, `attr.name="meshwork.type" attr.type="string"/>`, `attr.name="meshwork.type" attr.type="string"><default>Bool</default></key>`, 1)
	// A node-wide default also reaches transitions: explicitly clear their type.
	defaulted = strings.Replace(defaulted, `<data key="kind">transition</data>`, `<data key="kind">transition</data><data key="type"></data>`, 1)
	r, err = importText(t, defaulted, false)
	if err != nil || !bytes.Equal(semanticBytes(t, r), semanticBytes(t, full)) {
		t.Fatal("key default failed", err)
	}
}

func TestGraphMLNodeAndKeyOrder(t *testing.T) {
	text := graphMLFixture(t)
	nodeStart := strings.Index(text, `    <node id="n3"`)
	nodeEnd := nodeStart + strings.Index(text[nodeStart:], "\n") + 1
	node := text[nodeStart:nodeEnd]
	variant := text[:nodeStart] + text[nodeEnd:]
	variant = strings.Replace(variant, `    <node id="group"`, node+`    <node id="group"`, 1)
	key := `  <key id="impl" for="node" attr.name="meshwork.implementation" attr.type="string"/>`
	variant = strings.Replace(variant, key+"\n", "", 1)
	variant = strings.Replace(variant, `  <key id="kind"`, key+"\n"+`  <key id="kind"`, 1)
	a, err := importText(t, text, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := importText(t, variant, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(semanticBytes(t, a), semanticBytes(t, b)) {
		t.Fatal("XML declaration order changed computation")
	}
}
func TestGraphMLIsolatedNodesAndMetadataConflicts(t *testing.T) {
	text := graphMLFixture(t)
	noEdges := text[:strings.Index(text, `    <edge id="e0"`)] + "</graph></graphml>"
	r, err := importText(t, noEdges, false)
	if err != nil || len(r.Diagnostics) != 1 || !strings.Contains(r.Diagnostics[0], "inputs/outputs") {
		t.Fatal(r.Diagnostics, err)
	}
	if _, err := r.Graph.Instantiate(nil); err == nil {
		t.Fatal("isolated transition executed")
	}
	for _, property := range []string{"meshwork.implementation", "meshwork.architecture", "meshwork.entity"} {
		t.Run(property, func(t *testing.T) {
			root := t.TempDir()
			a := strings.Replace(text, "meshwork.implementation", property, 1)
			write(t, root, "a.graphml", a)
			write(t, root, "b.graphml", strings.Replace(a, ">builtin.nand<", ">different<", 1))
			files, err := DiscoverGraphML(repo(root), false)
			if err != nil {
				t.Fatal(err)
			}
			r, err := LinkFiles(files)
			if err == nil || !reflect.DeepEqual(r, Result{}) || !strings.Contains(err.Error(), "a.graphml#n2") || !strings.Contains(err.Error(), "b.graphml#n2") {
				t.Fatal("metadata conflict accepted", err)
			}
		})
	}
}

func TestGraphMLUsesExistingComposition(t *testing.T) {
	root := t.TempDir()
	write(t, root, "graph.nand.graphml", graphMLFixture(t))
	write(t, root, "graph.use.meshwork.yaml", "namespace: /use\nreferences: [{graph: test:graph.nand, namespace: first, bindings: {A: /inputs/A, B: /inputs/B, Q: /outputs/Q}}]\n")
	xml, err := DiscoverGraphML(repo(root), false)
	if err != nil {
		t.Fatal(err)
	}
	yaml, err := Discover(repo(root))
	if err != nil {
		t.Fatal(err)
	}
	result, err := LinkFiles(append(xml, yaml...))
	if err != nil {
		t.Fatal(err)
	}
	g := result.Definitions["/use"]
	if len(g.Places) != 3 || len(g.Transitions) != 1 || g.Transitions[0].Name != "/use/first/nand" || !reflect.DeepEqual(g.Transitions[0].Inputs, []string{"/inputs/A", "/inputs/B"}) || !reflect.DeepEqual(g.Transitions[0].InputPorts, []string{"A", "B"}) {
		t.Fatalf("GraphML bypassed composition: %+v", g)
	}
	_, registry := meshwork.LogicLibrary()
	registry["builtin.nand"] = registry["logic.nand.go"]
	rt, err := g.Instantiate(registry)
	if err != nil {
		t.Fatal(err)
	}
	token, _ := meshwork.NewToken("Bool", true)
	for _, p := range []string{"/inputs/A", "/inputs/B"} {
		if err := rt.Put(p, token); err != nil {
			t.Fatal(err)
		}
	}
	if err := rt.Fire(context.Background(), "/use/first/nand"); err != nil {
		t.Fatal(err)
	}
	out, ok := rt.Token("/outputs/Q")
	var q bool
	if !ok || out.Decode(&q) != nil || q {
		t.Fatal("composed imported NAND produced wrong result")
	}
	if _, err := CanonicalYAML(g); err != nil {
		t.Fatal(err)
	}
	// Remapped endpoint basenames that differ from preserved local ports cannot be exported losslessly.
	g.Transitions[0].InputPorts[0] = "renamed"
	if _, err := CanonicalYAML(g); err == nil {
		t.Fatal("lossy port export accepted")
	}
}
