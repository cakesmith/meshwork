package meshwork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func runGraph(t *testing.T, g ExecutableGraph, impl map[string]Compute, values map[string]bool) *Runtime {
	t.Helper()
	r, err := g.Instantiate(impl)
	if err != nil {
		t.Fatal(err)
	}
	for n, v := range values {
		tok, err := NewToken("Bool", v)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Put(n, tok); err != nil {
			t.Fatal(err)
		}
	}
	pending := append([]AtomicTransition(nil), g.Transitions...)
	for len(pending) > 0 {
		var next []AtomicTransition
		for _, tr := range pending {
			err := r.Fire(context.Background(), tr.Name)
			if errors.Is(err, ErrNotEnabled) {
				next = append(next, tr)
			} else if err != nil {
				t.Fatal(err)
			}
		}
		if len(next) == len(pending) {
			t.Fatal("deadlocked example")
		}
		pending = next
	}
	return r
}

func TestLogicTruthTables(t *testing.T) {
	l, impl := testLogicLibrary()
	for _, arch := range []string{"/logic/nand", "/logic/not", "/logic/or"} {
		for _, a := range []bool{false, true} {
			for _, b := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%t/%t", arch, a, b), func(t *testing.T) {
					g := testORGraph()
					g.References[0].Graph = arch
					values := map[string]bool{"/test/A": a, "/test/B": b}
					want := a || b
					if arch == "/logic/nand" {
						want = !(a && b)
					}
					if arch == "/logic/not" {
						delete(g.References[0].Bindings, "B")
						g.Places = []Place{{"A", "Bool"}, {"Q", "Bool"}}
						delete(values, "/test/B")
						want = !a
					}
					flat, err := l.Flatten(g, "/test")
					if err != nil {
						t.Fatal(err)
					}
					if arch == "/logic/or" && (len(flat.Transitions) != 5 || len(flat.Places) != 9) {
						t.Fatalf("unexpected OR graph: %+v", flat)
					}
					data, err := json.Marshal(flat)
					if err != nil {
						t.Fatal(err)
					}
					var restored ExecutableGraph
					if err := json.Unmarshal(data, &restored); err != nil {
						t.Fatal(err)
					}
					r := runGraph(t, restored, impl, values)
					tok, ok := r.Token("/test/Q")
					var got bool
					if !ok {
						t.Fatal("missing output")
					}
					if err := tok.Decode(&got); err != nil {
						t.Fatal(err)
					}
					if got != want {
						t.Fatalf("got %t want %t", got, want)
					}
					for _, s := range r.Snapshot() {
						if s.Name != "/test/Q" && s.State != "empty" {
							t.Fatalf("unconsumed intermediate: %+v", s)
						}
					}
				})
			}
		}
	}
}

func TestResolutionAndComposition(t *testing.T) {
	l, _ := testLogicLibrary()
	g := testORGraph()
	one, err := l.Compose("/test", g, g)
	if err != nil {
		t.Fatal(err)
	}
	single, err := l.Flatten(g, "/test")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(one, single) {
		t.Fatal("identical names must share objects")
	}
	g2 := testORGraph()
	g2.References[0].Namespace = "another"
	two, err := l.Compose("/test", g, g2)
	if err != nil {
		t.Fatal(err)
	}
	if len(two.Transitions) != 10 || len(two.Places) != 15 {
		t.Fatalf("distinct names must keep internal objects independent: %+v", two)
	}
	g = testORGraph()
	g.References = append(g.References, g2.References...)
	fwd, err := l.Flatten(g, "/test")
	if err != nil {
		t.Fatal(err)
	}
	g.References[0], g.References[1] = g.References[1], g.References[0]
	g.Places[0], g.Places[2] = g.Places[2], g.Places[0]
	rev, err := l.Flatten(g, "/test")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fwd, rev) {
		t.Fatal("order-dependent flattening")
	}
	for _, n := range []string{"/x/.", "/x/..", "/x//a", "/x/", "/x a", "/x\\a"} {
		if _, err := Resolve("/", n); err == nil {
			t.Fatalf("accepted %q", n)
		}
	}
	if n, err := Resolve("/scope", "/shared"); err != nil || n != "/shared" {
		t.Fatal(n, err)
	}
	if n, err := Resolve("/scope", "local"); err != nil || n != "/scope/local" {
		t.Fatal(n, err)
	}
}

func TestInvalidGraphs(t *testing.T) {
	for _, tc := range []struct {
		name, contains string
		change         func(*Graph, *Library)
	}{
		{"type conflict", "incompatible place", func(g *Graph, l *Library) { g.Places = append(g.Places, Place{"A", "Image"}) }},
		{"place transition collision", "name used", func(g *Graph, l *Library) { g.Places = append(g.Places, Place{"or/nand/nand", "Bool"}) }},
		{"unknown graph", "unknown graph", func(g *Graph, l *Library) { g.References[0].Graph = "/unknown" }},
		{"unknown binding", "unknown object", func(g *Graph, l *Library) { g.References[0].Bindings["missing"] = "A" }},
		{"duplicate input arc", "duplicate arc", func(g *Graph, l *Library) {
			g.References[0].Graph = "/logic/nand"
			g.References[0].Bindings["B"] = "A"
		}},
		{"conflicting transition", "conflicting graph binding", func(g *Graph, l *Library) {
			g.References[0].Graph = "/logic/nand"
			g.References = append(g.References, Reference{Namespace: "or", Graph: "/logic/nand", Bindings: map[string]string{"A": "B", "B": "A", "Q": "Q"}})
		}},
		{"recursive expansion", "recursive", func(g *Graph, l *Library) {
			l.Graphs["/logic/or"] = Graph{References: []Reference{{Graph: "/logic/or"}}}
		}},
		{"wrong bound type", "incompatible place", func(g *Graph, l *Library) { g.Places[0].Type = "Image" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := testORGraph()
			l, _ := testLogicLibrary()
			tc.change(&g, &l)
			_, err := l.Flatten(g, "/test")
			if err == nil || !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("expected %s, got %v", tc.contains, err)
			}
		})
	}
	r := New()
	if err := r.AddPlace(Place{"/p", "Bool"}); err != nil {
		t.Fatal(err)
	}
	fn := func(context.Context, map[string]Token) (map[string]Token, error) { return nil, nil }
	if err := r.AddTransition(Transition{Name: "/t", Inputs: []string{"/p"}, Outputs: []string{"/p"}, Run: fn}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddTransition(Transition{Name: "/u", Inputs: []string{"/t"}, Outputs: []string{"/p"}, Run: fn}); err == nil {
		t.Fatal("accepted transition-to-transition arc")
	}
	if err := r.AddPlace(Place{"/t", "Bool"}); err == nil {
		t.Fatal("accepted node kind collision")
	}
}

func TestInstantiateValidationAndIsolation(t *testing.T) {
	l, impl := testLogicLibrary()
	flat, err := l.Flatten(testORGraph(), "/test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := flat.Instantiate(nil); err == nil {
		t.Fatal("accepted unknown implementation")
	}
	bad := flat
	bad.Transitions = append([]AtomicTransition(nil), flat.Transitions...)
	bad.Transitions[0].InputPorts = nil
	if _, err := bad.Instantiate(impl); err == nil {
		t.Fatal("accepted malformed port mapping")
	}
	r, err := flat.Instantiate(impl)
	if err != nil {
		t.Fatal(err)
	}
	// Caller changes to the serialized graph must not change an existing runtime.
	first := flat.Transitions[0].Name
	flat.Transitions[0].Inputs[0] = "/missing"
	flat.Transitions[0].InputPorts[0] = "missing"
	tok, _ := NewToken("Bool", true)
	if err := r.Put("/test/A", tok); err != nil {
		t.Fatal(err)
	}
	if err := r.Fire(context.Background(), first); err != nil {
		t.Fatal(err)
	}
}

func TestGraphInstancesHaveOnlyExplicitNamespaces(t *testing.T) {
	l, _ := testLogicLibrary()
	g := Graph{References: []Reference{{Graph: "/logic/nand"}, {Graph: "/logic/nand"}}}
	one, err := l.Flatten(g, "/test")
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Places) != 3 || len(one.Transitions) != 1 || one.Places[0].Name != "/logic/nand/A" {
		t.Fatal("implicit occurrence namespace", one)
	}
	g.References[0].Namespace = "first"
	g.References[1].Namespace = "second"
	two, err := l.Flatten(g, "/test")
	if err != nil {
		t.Fatal(err)
	}
	if len(two.Places) != 6 || len(two.Transitions) != 2 {
		t.Fatal("explicit namespaces did not separate instances", two)
	}
	for _, tr := range two.Transitions {
		for _, endpoint := range append(append([]string(nil), tr.Inputs...), tr.Outputs...) {
			found := false
			for _, p := range two.Places {
				if p.Name == endpoint {
					found = true
				}
			}
			if !found {
				t.Fatal("non-bipartite flattened endpoint", endpoint)
			}
		}
	}
	before, _ := json.Marshal(l)
	g.References[1].Bindings = map[string]string{"A": "/shared"}
	linked, err := l.Link(g, "/test")
	if err != nil {
		t.Fatal(err)
	}
	reversed := Graph{References: []Reference{g.References[1], g.References[0]}}
	linked2, err := l.Link(reversed, "/test")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(linked)
	b, _ := json.Marshal(linked2)
	if string(a) != string(b) {
		t.Fatal("nondeterministic linked graph")
	}
	if _, err := l.Flatten(linked, "/test"); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(l)
	if string(before) != string(after) {
		t.Fatal("linking mutated graph library")
	}
}
