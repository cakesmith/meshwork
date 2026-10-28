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
	l, impl := LogicLibrary()
	for _, arch := range []string{"logic.nand:go", "logic.not:nand", "logic.or:nand"} {
		for _, a := range []bool{false, true} {
			for _, b := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%t/%t", arch, a, b), func(t *testing.T) {
					g := ORGraph()
					g.References[0].Architecture = arch
					values := map[string]bool{"/test/A": a, "/test/B": b}
					want := a || b
					if arch == "logic.nand:go" {
						want = !(a && b)
					}
					if arch == "logic.not:nand" {
						delete(g.References[0].Bindings, "B")
						g.Places = []Place{{"A", "Bool"}, {"Q", "Bool"}}
						delete(values, "/test/B")
						want = !a
					}
					flat, err := l.Flatten(g, "/test")
					if err != nil {
						t.Fatal(err)
					}
					if arch == "logic.or:nand" && (len(flat.Transitions) != 5 || len(flat.Places) != 9) {
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
	l, _ := LogicLibrary()
	g := ORGraph()
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
	g2 := ORGraph()
	g2.References[0].Name = "another"
	two, err := l.Compose("/test", g, g2)
	if err != nil {
		t.Fatal(err)
	}
	if len(two.Transitions) != 10 || len(two.Places) != 15 {
		t.Fatalf("distinct names must keep internal objects independent: %+v", two)
	}
	g = ORGraph()
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
		{"place transition collision", "name used", func(g *Graph, l *Library) { g.Places = append(g.Places, Place{"or/nand", "Bool"}) }},
		{"missing place", "requires declared", func(g *Graph, l *Library) { g.Places = g.Places[1:] }},
		{"wrong interface type", "requires declared", func(g *Graph, l *Library) { g.Places[0].Type = "Image" }},
		{"missing binding", "bindings", func(g *Graph, l *Library) { delete(g.References[0].Bindings, "A") }},
		{"unknown architecture", "unknown architecture", func(g *Graph, l *Library) { g.References[0].Architecture = "unknown" }},
		{"unknown entity", "unknown entity", func(g *Graph, l *Library) {
			a := l.Architectures["logic.or:nand"]
			a.Entity = "missing"
			l.Architectures["logic.or:nand"] = a
		}},
		{"duplicate input arc", "duplicate arc", func(g *Graph, l *Library) {
			g.References[0].Architecture = "logic.nand:go"
			g.References[0].Bindings["B"] = "A"
		}},
		{"conflicting transition", "conflicting transition", func(g *Graph, l *Library) {
			g.References[0].Architecture = "logic.nand:go"
			g.References = append(g.References, Reference{Name: "or", Architecture: "logic.nand:go", Bindings: map[string]string{"A": "B", "B": "A", "Q": "Q"}})
		}},
		{"recursive expansion", "recursive", func(g *Graph, l *Library) {
			a := l.Architectures["logic.or:nand"]
			a.Graph = &Graph{Places: g.Places, References: []Reference{{Name: "again", Architecture: "logic.or:nand", Bindings: map[string]string{"A": "A", "B": "B", "Q": "Q"}}}}
			l.Architectures["logic.or:nand"] = a
		}},
		{"incomplete implementation interface", "interface place", func(g *Graph, l *Library) {
			a := l.Architectures["logic.or:nand"]
			copyGraph := *a.Graph
			copyGraph.Places = copyGraph.Places[1:]
			a.Graph = &copyGraph
			l.Architectures["logic.or:nand"] = a
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := ORGraph()
			l, _ := LogicLibrary()
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
	l, impl := LogicLibrary()
	flat, err := l.Flatten(ORGraph(), "/test")
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
