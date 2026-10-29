package linker

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	distributed "example.com/meshwork-prototype/examples/distributed"
	"example.com/meshwork-prototype/meshwork"
)

func fixtureRepos() []Repository {
	return []Repository{{Name: "primitives", Root: "../examples/distributed/primitives", Namespace: "/projects/logic"}, {Name: "composites", Root: "../examples/distributed/composites", Namespace: "/projects/logic"}}
}
func write(t *testing.T, root, name, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}
func repo(root string) []Repository { return []Repository{{Name: "test", Root: root}} }

func TestDistributedExamples(t *testing.T) {
	result, err := Link(fixtureRepos())
	if err != nil {
		t.Fatal(err)
	}
	nand := result.Definitions["primitives:nand"]
	if len(nand.Places) != 3 || len(nand.Transitions) != 1 || len(nand.Transitions[0].Inputs) != 2 || len(nand.Transitions[0].Outputs) != 1 {
		t.Fatalf("bad NAND: %+v", nand)
	}
	or := result.Definitions["composites:or"]
	if len(or.Places) != 9 || len(or.Transitions) != 5 {
		t.Fatalf("bad OR: %+v", or)
	}
	if len(result.Scenarios) != 17 {
		t.Fatalf("got %d scenarios", len(result.Scenarios))
	}
	impl := distributed.Implementations()
	before, _ := json.Marshal(result)
	for _, s := range result.Scenarios {
		if _, err := RunScenario(context.Background(), result.Definitions[s.Graph], s, impl); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := json.Marshal(result)
	if string(before) != string(after) {
		t.Fatal("scenario mutated definitions")
	}
	// The same cross-directory definition also resolves inside one repository.
	root := t.TempDir()
	write(t, root, "one/graph.nand.meshwork.yaml", "namespace: /logic/nand\nplaces: {A: Bool, B: Bool, Q: Bool}\ntransitions: {nand: {implementation: logic.nand.go}}\narcs: [[A, nand], [B, nand], [nand, Q]]\n")
	write(t, root, "two/graph.or.meshwork.yaml", "namespace: /logic/or\nreferences: [{graph: nand}]\n")
	r, err := Link(repo(root))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Definitions["test:nand"], r.Definitions["test:or"]) {
		t.Fatal("unbound reference changed identity")
	}
}

func TestCrossDirectoryOR(t *testing.T) {
	root := t.TempDir()
	for _, f := range []struct{ from, to string }{
		{"primitives/inputs/graph.inputs.meshwork.yaml", "inputs/graph.inputs.meshwork.yaml"},
		{"primitives/implementation/graph.nand.meshwork.yaml", "nand/graph.nand.meshwork.yaml"},
		{"primitives/fanout/graph.duplicate.meshwork.yaml", "dup/graph.duplicate.meshwork.yaml"},
		{"composites/logic/graph.or.meshwork.yaml", "or/graph.or.meshwork.yaml"},
	} {
		b, err := os.ReadFile(filepath.Join("../examples/distributed", f.from))
		if err != nil {
			t.Fatal(err)
		}
		write(t, root, f.to, string(b))
	}
	result, err := Link([]Repository{{Name: "primitives", Root: root}})
	if err != nil {
		t.Fatal(err)
	}
	g := result.Definitions["primitives:or"]
	if len(g.Places) != 9 || len(g.Transitions) != 5 {
		t.Fatalf("unexpected cross-directory OR: %+v", g)
	}
	impl := distributed.Implementations()
	for _, a := range []bool{false, true} {
		for _, b := range []bool{false, true} {
			ta, _ := meshwork.NewToken("Bool", a)
			tb, _ := meshwork.NewToken("Bool", b)
			q, _ := meshwork.NewToken("Bool", a || b)
			s := Scenario{Name: "or", Marking: map[string]meshwork.Token{"/logic/or/A": ta, "/logic/or/B": tb}, Expect: map[string]meshwork.Token{"/logic/or/Q": q}}
			if _, err := RunScenario(context.Background(), g, s, impl); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestDiscoveryAndDeterminism(t *testing.T) {
	files, err := Discover(fixtureRepos())
	if err != nil {
		t.Fatal(err)
	}
	want, err := LinkFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(want)
	rng := rand.New(rand.NewSource(17))
	for i := 0; i < 30; i++ {
		rng.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })
		got, err := LinkFiles(files)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(got)
		if string(b) != string(wantJSON) {
			t.Fatal("discovery order affected canonical output")
		}
	}
	root := t.TempDir()
	for _, name := range []string{"graph.ok.meshwork.yaml", "nested/run.scenario.ok.meshwork.yaml", "nested/tests.scenarios.ok.meshwork.yaml", "other.meshwork.yaml", "graph.bad.extra.meshwork.yaml", "foo.scenario.x.json", ".git/graph.hidden.meshwork.yaml"} {
		write(t, root, name, "{}")
	}
	f, err := Discover(repo(root))
	if err != nil || len(f) != 3 {
		t.Fatalf("discovery: %v %v", f, err)
	}
}

func TestConflictsAndTopology(t *testing.T) {
	base := "namespace: /n\nplaces: {A: Bool, B: Bool, Q: Bool}\ntransitions: {nand: {implementation: logic.nand.go}}\narcs: [[A, nand], [B, nand], [nand, Q]]\n"
	for _, tc := range []struct{ name, extra, want string }{
		{"place type", "namespace: /n\nplaces: {A: Image}", "type:\"Image\""},
		{"implementation", "namespace: /n\ntransitions: {nand: {implementation: wrong}}", "implementation:\"wrong\""},
		{"node kind", "namespace: /n\nplaces: {nand: Bool}", "conflicting identity /n/nand"},
		{"place arc", "namespace: /n\narcs: [[A, Q]]", "non-bipartite arc /n/A -> /n/Q"},
		{"transition arc", "namespace: /n\ntransitions: {other: {implementation: other}}\narcs: [[nand, other]]", "non-bipartite arc /n/nand -> /n/other"},
		{"missing endpoint", "namespace: /n\narcs: [[missing, nand]]", "unresolved arc /n/missing -> /n/nand"},
		{"missing definition", "namespace: /n\nreferences: [{graph: absent:nand}]", "unresolved repository definition"},
		{"recursive", "namespace: /n\nreferences: [{graph: test:base}]", "recursive graph reference"},
		{"unknown binding", "namespace: /other\nreferences: [{graph: test:base, bindings: {missing: A}}]", "unknown object /n/missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "graph.base.meshwork.yaml", base)
			write(t, root, "graph.extra.meshwork.yaml", tc.extra)
			result, err := Link(repo(root))
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), ".meshwork.yaml:") {
				t.Fatalf("want %q with location, got %v", tc.want, err)
			}
			if !reflect.DeepEqual(result, Result{}) {
				t.Fatalf("failed link returned partial result: %+v", result)
			}
		})
	}
	root := t.TempDir()
	write(t, root, "graph.base.meshwork.yaml", base)
	write(t, root, "graph.conflicts.meshwork.yaml", "namespace: /n\nplaces: {A: Image}\ntransitions: {nand: {implementation: other}}")
	_, err := Link(repo(root))
	if err == nil || !strings.Contains(err.Error(), "/n/A") || !strings.Contains(err.Error(), "/n/nand") || !strings.Contains(err.Error(), "graph.base.meshwork.yaml:2") || !strings.Contains(err.Error(), "graph.conflicts.meshwork.yaml:3") {
		t.Fatalf("missing conflict provenance: %v", err)
	}
}

func TestMergeBindingsAndImplementationOrigin(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a/graph.inputs.meshwork.yaml", "namespace: /logic/nand\nplaces: {A: Bool, B: Bool, Q: Bool}")
	write(t, root, "b/graph.nand.meshwork.yaml", "namespace: /logic/nand\ntransitions: {nand: {implementation: './Nand.kt#compute'}}\narcs: [[A, nand], [B, nand], [nand, Q], [A, nand]]")
	write(t, root, "c/graph.shared.meshwork.yaml", "namespace: /another\nbindings: {shared: /logic/nand/A}\nplaces: {shared: Bool}\narcs: [[shared, /logic/nand/nand]]")
	r, err := Link(repo(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Graph.Places) != 3 || len(r.Graph.Transitions) != 1 || len(r.Graph.Transitions[0].Inputs) != 2 {
		t.Fatalf("merge failed: %+v", r.Graph)
	}
	want := "./Nand.kt#compute"
	if r.Graph.Transitions[0].Implementation != want {
		t.Fatalf("got %q want %q", r.Graph.Transitions[0].Implementation, want)
	}
}

func TestRepositoryDefaultsAndExplicitOccurrences(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, root := range []string{a, b} {
		write(t, root, "graph.inputs.meshwork.yaml", "places: {A: Bool}")
	}
	repos := []Repository{{"primitives", a, "/projects/logic"}, {"composites", b, "/projects/logic"}}
	r, err := Link(repos)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Graph.Places) != 2 || r.Graph.Places[0].Name != "/projects/logic/composites/A" || r.Graph.Places[1].Name != "/projects/logic/primitives/A" {
		t.Fatalf("repository identity lost: %+v", r.Graph)
	}
	for _, root := range []string{a, b} {
		write(t, root, "graph.inputs.meshwork.yaml", "bindings: {A: /shared/A}\nplaces: {A: Bool}")
	}
	r, err = Link(repos)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Graph.Places) != 1 {
		t.Fatal("explicit binding did not share")
	}
	root := t.TempDir()
	write(t, root, "graph.nand.meshwork.yaml", "namespace: /template\nplaces: {A: Bool, B: Bool, Q: Bool}\ntransitions: {nand: {implementation: logic.nand.go}}\narcs: [[A, nand], [B, nand], [nand, Q]]")
	write(t, root, "graph.two.meshwork.yaml", "namespace: /two\nreferences:\n  - {graph: nand, namespace: left}\n  - {graph: nand, namespace: right}\n")
	r, err = Link(repo(root))
	if err != nil {
		t.Fatal(err)
	}
	g := r.Definitions["test:two"]
	if len(g.Places) != 6 || len(g.Transitions) != 2 {
		t.Fatalf("occurrences not independent: %+v", g)
	}
	for _, p := range g.Places {
		if !strings.HasPrefix(p.Name, "/two/left/") && !strings.HasPrefix(p.Name, "/two/right/") {
			t.Fatalf("unexpected identity %s", p.Name)
		}
	}
}

func TestResolution(t *testing.T) {
	for _, tc := range []struct{ ns, name, want string }{{"/a/b", "../c", "/a/c"}, {"/a/b", "./c", "/a/b/c"}, {"/a", "/shared", "/shared"}, {"/", "name", "/name"}} {
		got, err := resolveName(tc.ns, tc.name)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %s %v", tc, got, err)
		}
	}
	for _, name := range []string{"../../x", "x//y", "x/", "x y", "x\\y", ""} {
		if _, err := resolveName("/a", name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}

func TestParserAndScenarioValidation(t *testing.T) {
	base := "namespace: /n\nplaces: {A: Bool, B: Bool, Q: Bool}\ntransitions: {nand: {implementation: logic.nand.go}}\narcs: [[A, nand], [B, nand], [nand, Q]]"
	for _, tc := range []struct{ name, text, want string }{
		{"run.scenario.nand.meshwork.yaml", "graph: /n\nscenarios: []", "invalid in scenario"},
		{"tests.scenarios.nand.meshwork.yaml", "graph: /n\nname: wrong", "invalid in scenarios"},
		{"tests.scenarios.nand.meshwork.yaml", "graph: /n\nscenarios: []", "must not be empty"},
		{"run.scenario.nand.meshwork.yaml", "graph: /n\nname: one\nmarking: {missing: true}", "unknown place /n/missing"},
		{"tests.scenarios.nand.meshwork.yaml", "graph: /n\nscenarios: [{name: x}, {name: x}]", "duplicate scenario"},
		{"graph.bad.meshwork.yaml", "namespace: /n\nplaces: {A: Bool, A: Image}", "already defined"},
		{"graph.bad.meshwork.yaml", "unknown: true", "field unknown not found"},
		{"graph.bad.meshwork.yaml", "transitions: {t: {typo: x}}", "field typo not found"},
		{"graph.bad.meshwork.yaml", "{}\n---\n{}", "one YAML document"},
		{"graph.bad.meshwork.yaml", "arcs: [[A]]", "two endpoints"},
		{"graph.bad.meshwork.yaml", "bindings: {'A': '', './A': /other}", "invalid computational name"},
		{"graph.bad.meshwork.yaml", "bindings: {A: /one, './A': /other}", "conflicting binding"},
		{"graph.bad.meshwork.yaml", "transitions: {t: {}}", "implementation is required"},
		{"graph.bad.meshwork.yaml", "places: {A: ''}", "invalid declaration"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "graph.nand.meshwork.yaml", base)
			write(t, root, tc.name, tc.text)
			_, err := Link(repo(root))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q got %v", tc.want, err)
			}
		})
	}
}

func TestArcsOnlyContributionAndRelativeReferences(t *testing.T) {
	root := t.TempDir()
	write(t, root, "graph.nodes.meshwork.yaml", "namespace: /logic/nand\nplaces: {A: Bool, B: Bool, Q: Bool}\ntransitions: {nand: {implementation: logic.nand.go}}")
	write(t, root, "nested/graph.wiring.meshwork.yaml", "namespace: /wiring\narcs: [[/logic/nand/A, /logic/nand/nand], [/logic/nand/B, /logic/nand/nand], [/logic/nand/nand, /logic/nand/Q]]")
	write(t, root, "other/graph.use.meshwork.yaml", "namespace: /logic/use\nreferences: [{graph: ../nand}]")
	r, err := Link(repo(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"test:nodes", "test:wiring", "test:use"} {
		g := r.Definitions[h]
		if len(g.Places) != 3 || len(g.Transitions) != 1 || len(g.Transitions[0].Inputs) != 2 {
			t.Fatalf("incomplete selection for %s: %+v", h, g)
		}
	}
}

func TestScenarioFailuresAndIsolation(t *testing.T) {
	r, err := Link(fixtureRepos())
	if err != nil {
		t.Fatal(err)
	}
	s := r.Scenarios[0]
	g := r.Definitions[s.Graph]
	impl := distributed.Implementations()
	if _, err := RunScenario(context.Background(), g, s, nil); err == nil {
		t.Fatal("accepted missing implementation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunScenario(ctx, g, s, impl); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	empty := s
	empty.Marking = nil
	if _, err := RunScenario(context.Background(), g, empty, impl); err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("deadlock: %v", err)
	}
	wrong := s
	wrong.Expect = map[string]meshwork.Token{}
	for n, tok := range s.Expect {
		var b bool
		if err := tok.Decode(&b); err != nil {
			t.Fatal(err)
		}
		wrong.Expect[n], _ = meshwork.NewToken("Bool", !b)
	}
	if _, err := RunScenario(context.Background(), g, wrong, impl); err == nil || !strings.Contains(err.Error(), "expectation failed") {
		t.Fatalf("expectation: %v", err)
	}
	if _, err := RunScenario(context.Background(), g, s, impl); err != nil {
		t.Fatalf("failed run changed later scenario: %v", err)
	}
}
