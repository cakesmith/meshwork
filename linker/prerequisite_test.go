package linker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	distributed "example.com/meshwork-prototype/examples/distributed"
	"example.com/meshwork-prototype/meshwork"
)

func TestIndependentDiscoveryUnifies(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write(t, a, "nodes/graph.nodes.meshwork.yaml", "namespace: /shared\nplaces: {A: Bool, B: Bool, Q: Bool}\ntransitions: {nand: {implementation: logic.nand.go}}")
	write(t, b, "wiring/graph.wiring.meshwork.yaml", "namespace: /shared\nplaces: {A: Bool}\ntransitions: {nand: {implementation: logic.nand.go}}\narcs: [[A, nand], [B, nand], [nand, Q], [A, nand]]")
	repos := []Repository{{Name: "nodes", Root: a}, {Name: "wiring", Root: b}}
	var files []File
	for _, r := range repos {
		found, err := Discover([]Repository{r})
		if err != nil || len(found) != 1 {
			t.Fatalf("independent discovery: %v %v", found, err)
		}
		files = append(files, found...)
	}
	before := append([]File(nil), files...)
	result, err := LinkFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, before) {
		t.Fatal("link mutated discovery list")
	}
	g := result.Graph
	if len(g.Places) != 3 || len(g.Transitions) != 1 || len(g.Transitions[0].Inputs) != 2 || len(g.Transitions[0].Outputs) != 1 || len(g.Transitions[0].Sources) != 2 {
		t.Fatalf("contributions did not unify/deduplicate: %+v", g)
	}
	if !reflect.DeepEqual(result.Definitions["nodes:nodes"], result.Definitions["wiring:wiring"]) {
		t.Fatal("handles did not select the same merged region")
	}
	combined, err := Link(repos)
	if err != nil || !reflect.DeepEqual(result, combined) {
		t.Fatalf("separate discovery differs from combined discovery: %v", err)
	}
	for _, conflict := range []string{"places: {A: Image}", "transitions: {nand: {implementation: wrong}}", "places: {nand: Bool}"} {
		t.Run(conflict, func(t *testing.T) {
			write(t, b, "wiring/graph.wiring.meshwork.yaml", "namespace: /shared\n"+conflict)
			contents := make(map[string]string)
			for _, f := range files {
				data, err := os.ReadFile(f.Path)
				if err != nil {
					t.Fatal(err)
				}
				contents[f.Path] = string(data)
			}
			var diagnostic string
			for _, order := range [][]File{files, {files[1], files[0]}} {
				got, err := LinkFiles(order)
				if err == nil || !strings.Contains(err.Error(), "conflicting identity /shared/") || !strings.Contains(err.Error(), files[0].Path+":") || !strings.Contains(err.Error(), files[1].Path+":") {
					t.Fatalf("missing conflict/provenance: %v", err)
				}
				if !reflect.DeepEqual(got, Result{}) {
					t.Fatalf("conflict returned partial result: %+v", got)
				}
				if diagnostic != "" && diagnostic != err.Error() {
					t.Fatal("discovery order changed conflict diagnostics")
				}
				diagnostic = err.Error()
			}
			for path, want := range contents {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != want {
					t.Fatalf("failed link modified source %s: %v", path, err)
				}
			}
		})
	}
}

func TestReusableMUXTruthTable(t *testing.T) {
	result, err := Link(fixtureRepos())
	if err != nil {
		t.Fatal(err)
	}
	g := result.Definitions["composites:mux"]
	if len(g.Places) != 23 || len(g.Transitions) != 14 {
		t.Fatalf("unexpected MUX expansion: %+v", g)
	}
	impl := distributed.Implementations()
	for _, tr := range g.Transitions {
		if tr.Implementation != "logic.nand.go" && tr.Implementation != "logic.duplicate.go" {
			t.Fatalf("MUX introduced a special implementation: %+v", tr)
		}
	}
	// Run the serialized graph to verify local ports survive nested rebinding.
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	var restored meshwork.ExecutableGraph
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	for _, a := range []bool{false, true} {
		for _, b := range []bool{false, true} {
			for _, selectB := range []bool{false, true} {
				t.Run(fmt.Sprintf("A=%t/B=%t/S=%t", a, b, selectB), func(t *testing.T) {
					s := Scenario{Name: "mux", Marking: map[string]meshwork.Token{}, Expect: map[string]meshwork.Token{}}
					for name, value := range map[string]bool{"A": a, "B": b, "S": selectB} {
						token, err := meshwork.NewToken("Bool", value)
						if err != nil {
							t.Fatal(err)
						}
						s.Marking["/logic/mux/"+name] = token
					}
					want := a
					if selectB {
						want = b
					}
					q, err := meshwork.NewToken("Bool", want)
					if err != nil {
						t.Fatal(err)
					}
					s.Expect["/logic/mux/Q"] = q
					r, err := RunScenario(context.Background(), restored, s, impl)
					if err != nil {
						t.Fatal(err)
					}
					for _, p := range r.Snapshot() {
						if p.Name != "/logic/mux/Q" && p.State != "empty" {
							t.Fatalf("unconsumed input/intermediate: %+v", p)
						}
					}
				})
			}
		}
	}
}
