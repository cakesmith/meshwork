package linker

import (
	"reflect"
	"strings"
	"testing"
)

func TestExplicitPortsTransportCompositionAndExport(t *testing.T) {
	root := t.TempDir()
	write(t, root, "nested/graph.child.meshwork.yaml", `namespace: /child
places: {source: {type: Bool}, sink: Bool}
transitions: {work: {implementation: echo}}
arcs:
  - {from: source, to: work, port: value, transport: http-json}
  - {from: work, to: sink, port: result, transport: http-json}
`)
	write(t, root, "elsewhere/graph.parent.meshwork.yaml", `namespace: /parent
places: {input: Bool, output: Bool}
references:
  - graph: child
    namespace: /instance
    bindings: {source: /parent/input, sink: /parent/output}
`)
	r, err := Link(repo(root))
	if err != nil {
		t.Fatal(err)
	}
	g := r.Definitions["/parent"]
	tr := g.Transitions[0]
	if tr.Inputs[0] != "/parent/input" || tr.InputPorts[0] != "value" || tr.OutputPorts[0] != "result" || tr.InputTransports["value"] != "http-json" || tr.OutputTransports["result"] != "http-json" {
		t.Fatalf("lost binding: %+v", tr)
	}
	if _, err := g.Instantiate(nil); err == nil || !strings.Contains(err.Error(), "requires an executor binding") {
		t.Fatalf("native runtime ignored transport: %v", err)
	}
	data, err := CanonicalYAML(g)
	if err != nil {
		t.Fatal(err)
	}
	exported := t.TempDir()
	write(t, exported, "graph.roundtrip.meshwork.yaml", string(data))
	reloaded, err := Link(repo(exported))
	if err != nil {
		t.Fatal(err)
	}
	diff, err := Diff(g, reloaded.Graph)
	if err != nil || len(diff.SemanticChanges) != 0 {
		t.Fatalf("lossy export: %+v %v", diff, err)
	}
	changed := reloaded.Graph
	changed.Transitions[0].InputTransports = map[string]string{"value": "memory"}
	diff, err = Diff(g, changed)
	if err != nil || len(diff.SemanticChanges) != 2 || diff.SemanticChanges[0].Element != "arc" {
		t.Fatalf("transport not compared: %+v %v", diff, err)
	}
}

func TestExplicitPortValidationAndConflicts(t *testing.T) {
	base := "namespace: /g\nplaces: {A: Bool, B: Bool, Q: Bool}\ntransitions: {t: {implementation: echo}}\n"
	for _, body := range []string{
		"arcs: [{from: A, to: t, port: value, typo: true}, [t,Q]]",
		"arcs: [{from: A, to: t, port: ''}, [t,Q]]",
		"arcs: [{from: A, to: t, transport: 'invalid identity'}, [t,Q]]",
		"arcs: [{from: A, to: t, port: value}, {from: B, to: t, port: value}, [t,Q]]",
		"arcs: [{from: A, to: t, port: one}, {from: A, to: t, port: two}, [t,Q]]",
		"arcs: [{from: A, to: t, transport: memory}, {from: A, to: t, transport: http-json}, [t,Q]]",
		"arcs: [{from: A, to: Q, port: value}, [t,Q]]",
	} {
		t.Run(body, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "graph.bad.meshwork.yaml", base+body)
			r, err := Link(repo(root))
			if err == nil || !reflect.DeepEqual(r, Result{}) {
				t.Fatalf("accepted invalid graph or returned partial result: %+v %v", r, err)
			}
		})
	}
	root := t.TempDir()
	write(t, root, "graph.bad.meshwork.yaml", "places: {A: {type: Bool, extra: true}}")
	if _, err := Link(repo(root)); err == nil {
		t.Fatal("accepted unknown place field")
	}
}

func TestLinkerPreservesTransportIdentityWithoutLoadingIt(t *testing.T) {
	root := t.TempDir()
	write(t, root, "graph.transport.meshwork.yaml", `namespace: /g
places: {A: Bool, Q: Bool}
transitions: {work: {implementation: arbitrary-effect}}
arcs:
  - {from: A, to: work, port: value, transport: vendor.transport/v1}
  - [work, Q]
`)
	r, err := Link(repo(root))
	if err != nil {
		t.Fatal(err)
	}
	if r.Graph.Transitions[0].InputTransports["value"] != "vendor.transport/v1" {
		t.Fatal("lost transport identity")
	}
	if _, err := r.Graph.Instantiate(nil); err == nil {
		t.Fatal("native execution silently ignored transport")
	}
}
