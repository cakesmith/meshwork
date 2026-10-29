package linker

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"example.com/meshwork-prototype/meshwork"
)

func TestManualClockScenarioFixture(t *testing.T) {
	r, err := Link([]Repository{{Name: "timing", Root: "../examples/timing"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Scenarios) != 2 {
		t.Fatal(r.Scenarios)
	}
	before := cloneResult(t, r)
	for repeat := 0; repeat < 40; repeat++ {
		for _, s := range r.Scenarios {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			runtime, err := RunScenario(ctx, r.Definitions[s.Graph], s, meshwork.TimingImplementations())
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range runtime.Snapshot() {
				if strings.Contains(p.State, "locked") || strings.Contains(p.State, "reserved") {
					t.Fatal("scenario left reservations", p)
				}
			}
		}
	}
	if !reflect.DeepEqual(before, r) {
		t.Fatal("scenario modified definitions")
	}
}
func TestTimeoutDeclarationsAndExport(t *testing.T) {
	base := "namespace: /n\nplaces: {A: Bool, Q: Bool}\ntransitions: {work: {implementation: work%s}}\narcs: [[A,work],[work,Q]]\n"
	for _, tc := range []struct {
		value   string
		want    time.Duration
		invalid bool
	}{
		{"", 0, false}, {", timeout: infinite", 0, false}, {", timeout: 30s", 30 * time.Second, false},
		{", timeout: -1s", 0, true}, {", timeout: nonsense", 0, true}, {", timeout: 7", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			root := t.TempDir()
			text := strings.Replace(base, "%s", tc.value, 1)
			write(t, root, "graph.n.meshwork.yaml", text)
			r, err := Link(repo(root))
			if tc.invalid {
				if err == nil || !reflect.DeepEqual(r, Result{}) {
					t.Fatal("invalid timeout accepted", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.Graph.Transitions[0].Timeout != tc.want {
				t.Fatal(r.Graph)
			}
			data, err := CanonicalYAML(r.Graph)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == 0 && bytes.Contains(data, []byte("timeout:")) {
				t.Fatal("serialized unnecessary infinity")
			}
			write(t, root, "graph.n.meshwork.yaml", string(data))
			restored, err := Link(repo(root))
			if err != nil {
				t.Fatal(err)
			}
			if len(compare(t, r, restored).SemanticChanges) != 0 {
				t.Fatal("timeout lost on round trip")
			}
		})
	}
	root := t.TempDir()
	write(t, root, "graph.n.meshwork.yaml", strings.Replace(base, "%s", ", timeout: 30s", 1))
	write(t, root, "graph.other.meshwork.yaml", strings.Replace(base, "%s", ", timeout: 20s", 1))
	r, err := Link(repo(root))
	if err == nil || !reflect.DeepEqual(r, Result{}) || !strings.Contains(err.Error(), "conflicting identity") {
		t.Fatal("conflicting timeouts accepted", err)
	}
}
func TestGraphMLTimeoutAndObsoleteContracts(t *testing.T) {
	base := graphMLFixture(t)
	key := `<key id="timeout" for="node" attr.name="meshwork.timeout" attr.type="string"/>`
	timed := strings.Replace(base, `<key id="kind"`, key+`<key id="kind"`, 1)
	timed = strings.Replace(timed, `<data key="impl">builtin.nand</data>`, `<data key="impl">builtin.nand</data><data key="timeout">30s</data>`, 1)
	r, err := importText(t, timed, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Graph.Transitions[0].Timeout != 30*time.Second {
		t.Fatal(r.Graph)
	}
	original, err := importText(t, base, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(compare(t, original, r)); !reflect.DeepEqual(got, []string{"transition_definition_changed"}) {
		t.Fatal(got)
	}
	for _, bad := range []string{"-1s", "bad"} {
		if _, err := importText(t, strings.Replace(timed, ">30s<", ">"+bad+"<", 1), false); err == nil {
			t.Fatal("invalid GraphML duration accepted")
		}
	}
	for _, name := range []string{"meshwork.entity", "meshwork.architecture"} {
		if _, err := importText(t, strings.Replace(base, "meshwork.implementation", name, 1), false); err == nil {
			t.Fatal("obsolete contract accepted")
		}
	}
}
func TestScenariosCannotRedefineComputation(t *testing.T) {
	base := "namespace: /n\nplaces: {A: Bool, Q: Bool}\ntransitions: {work: {implementation: work}}\narcs: [[A,work],[work,Q]]\n"
	for _, field := range []string{"references: [{graph: /other}]", "transitions: {work: {implementation: other}}", "places: {A: Image}", "arcs: [[A,Q]]", "implementation: other", "timeout: 30s"} {
		for _, collection := range []bool{false, true} {
			root := t.TempDir()
			write(t, root, "graph.n.meshwork.yaml", base)
			name, text := "run.scenario.n.meshwork.yaml", "graph: /n\nname: no\n"+field
			if collection {
				name = "tests.scenarios.n.meshwork.yaml"
				text = "graph: /n\nscenarios:\n  - name: no\n    " + field
			}
			write(t, root, name, text)
			r, err := Link(repo(root))
			if err == nil || !reflect.DeepEqual(r, Result{}) {
				t.Fatal("scenario changed computation", text, err)
			}
		}
	}
}
func TestScenarioClockValidation(t *testing.T) {
	base := "namespace: /n\nplaces: {A: Bool}\n"
	for _, extra := range []string{
		"clock: {type: unknown}", "clock: {type: real, start: 1s}", "clock: {type: manual, start: -1s}", "clock: {type: manual, start: infinite}", "clock: {start: 1s}",
		"steps: [{advance_clock: 1s}]", "clock: {type: manual}\nsteps: [{advance_clock: -1s}]",
		"clock: {type: manual}\nsteps: [{expect: {missing: empty}}]",
		"clock: {type: manual}\nsteps: [{advance_clock: 1s, expect: {A: empty}}]",
		"clock: {type: manual}\nsteps: [{}]", "clock: {type: manual}\nsteps: [{advance_clock: infinite}]",
	} {
		root := t.TempDir()
		write(t, root, "graph.n.meshwork.yaml", base)
		write(t, root, "run.scenario.n.meshwork.yaml", "graph: /n\nname: bad\n"+extra)
		r, err := Link(repo(root))
		if err == nil || !reflect.DeepEqual(r, Result{}) {
			t.Fatal("invalid clock/step accepted", extra, err)
		}
	}
}
func TestDiffLinkedReferencesAndStages(t *testing.T) {
	library := meshwork.Library{Graphs: map[string]meshwork.Graph{
		"/child": {Places: []meshwork.Place{{Name: "A", Type: "Bool"}, {Name: "Q", Type: "Bool"}}, Transitions: []meshwork.AtomicTransition{{Name: "work", Implementation: "test.work", Inputs: []string{"A"}, Outputs: []string{"Q"}, InputPorts: []string{"A"}, OutputPorts: []string{"Q"}}}},
	}}
	root := meshwork.Graph{Places: []meshwork.Place{{Name: "A", Type: "Bool"}, {Name: "B", Type: "Bool"}, {Name: "Q", Type: "Bool"}}, References: []meshwork.Reference{{Graph: "/child", Namespace: "child", Bindings: map[string]string{"A": "A", "Q": "Q"}}}}
	a, err := library.Link(root, "/test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := library.Link(root, "/test")
	if err != nil {
		t.Fatal(err)
	}
	b.References[0].Bindings["/child/A"] = "/test/B"
	report, err := Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kinds(report), []string{"reference_binding_changed"}) {
		t.Fatal(report)
	}
	b.References[0].Graph = "/other"
	report, err = Diff(a, b)
	if err != nil || len(report.SemanticChanges) != 2 {
		t.Fatal(report, err)
	}
	flat, err := library.Flatten(root, "/test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Diff(a, flat); err == nil {
		t.Fatal("mixed resolution stages compared")
	}
	b = a
	b.References = nil
	report, err = Diff(a, b)
	if err != nil || !reflect.DeepEqual(kinds(report), []string{"reference_removed"}) {
		t.Fatal(report, err)
	}
}
