package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var errStopStream = errors.New("stop stream")

type stopWriter struct{ bytes.Buffer }

func (w *stopWriter) Write(p []byte) (int, error) {
	_, _ = w.Buffer.Write(p)
	return len(p), errStopStream
}

func TestCommands(t *testing.T) {
	for _, cmd := range []string{"discover", "link", "validate"} {
		var out bytes.Buffer
		err := run([]string{cmd, "-repo", "primitives=../../examples/distributed/primitives", "-repo", "composites=../../examples/distributed/composites"}, &out)
		if err != nil {
			t.Fatal(err)
		}
		if out.Len() == 0 {
			t.Fatal("empty output")
		}
		if cmd == "validate" && !strings.Contains(out.String(), "17 scenarios") {
			t.Fatal(out.String())
		}
	}
	for _, args := range [][]string{nil, {"unknown"}, {"link", "-repo", "bad"}, {"link", "-unknown"}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestConflictsHaltOutput(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"graph.a.meshwork.yaml": "namespace: /shared\nplaces: {A: Bool}",
		"graph.b.meshwork.yaml": "namespace: /shared\nplaces: {A: Image}",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range []string{"link", "validate"} {
		var out bytes.Buffer
		err := run([]string{command, root}, &out)
		if err == nil || !strings.Contains(err.Error(), "conflicting identity /shared/A") {
			t.Fatalf("%s did not reject conflict: %v", command, err)
		}
		if out.Len() != 0 {
			t.Fatalf("%s emitted output after conflict: %s", command, out.String())
		}
	}
}

func TestExecuteScenarioFile(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"execute", "--scenario", "../../examples/distributed/composites/tests/tests.scenarios.mux.meshwork.yaml"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(out.String(), "passed: "); got != 8 {
		t.Fatalf("got %d results: %s", got, out.String())
	}
	if err := run([]string{"execute", "-repo", "primitives=../../examples/distributed/primitives"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "requires --scenario") {
		t.Fatal("execute accepted no scenario", err)
	}
}

func TestExecuteStdinScenario(t *testing.T) {
	out := &stopWriter{}
	err := runInput([]string{"execute", "--scenario", "../../examples/distributed/composites/run.scenario.nand.meshwork.yaml"}, strings.NewReader("{\"place\":\"A\",\"value\":true}\n{\"place\":\"B\",\"value\":true}\n"), out)
	if !errors.Is(err, errStopStream) || out.String() != "{\"place\":\"/logic/nand/Q\",\"value\":false}\n" {
		t.Fatalf("output %q, error %v", out.String(), err)
	}
	for _, input := range []string{"{A: true}", "{\"place\":\"extra\",\"value\":false}", "{\"place\":\"A\"}"} {
		if err := runInput([]string{"execute", "--scenario", "../../examples/distributed/composites/run.scenario.nand.meshwork.yaml"}, strings.NewReader(input), &bytes.Buffer{}); err == nil {
			t.Fatal("accepted invalid stdin", input)
		}
	}
}

func TestGraphMLCommands(t *testing.T) {
	fixture := "../../linker/testdata/graphml/graph.nand.graphml"
	for _, args := range [][]string{
		{"import", fixture}, {"import", fixture, "--format", "yaml"}, {"import", "--format", "json", fixture}, {"validate", fixture},
		{"link", "../../linker/testdata/graphml", "--graphml-root", "../../linker/testdata/graphml"},
		{"discover", "../../linker/testdata/graphml", "--graphml-root", "../../linker/testdata/graphml"},
	} {
		var out bytes.Buffer
		if err := run(args, &out); err != nil {
			t.Fatal(args, err)
		}
		if out.Len() == 0 {
			t.Fatal("empty output", args)
		}
		if len(args) == 2 && args[0] == "import" && !strings.Contains(out.String(), "Status: Valid") {
			t.Fatal(out.String())
		}
	}
	for _, args := range [][]string{{"import"}, {"import", fixture, fixture}, {"import", fixture, "--format", "bad"}} {
		var out bytes.Buffer
		if err := run(args, &out); err == nil || out.Len() != 0 {
			t.Fatal("accepted bad import", args, err)
		}
	}
	b, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	partial := strings.Replace(string(b), `<data key="impl">builtin.nand</data>`, "", 1)
	p := filepath.Join(t.TempDir(), "partial.graphml")
	if err := os.WriteFile(p, []byte(partial), 0644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"import", p}, &out); err != nil || !strings.Contains(out.String(), "Status: Incomplete") {
		t.Fatal(out.String(), err)
	}
	out.Reset()
	if err := run([]string{"validate", p}, &out); err == nil || !strings.Contains(out.String(), "missing transition implementation") {
		t.Fatal("incomplete validated", out.String(), err)
	}
	invalid := strings.Replace(string(b), `source="n0" target="n2"`, `source="n0" target="n1"`, 1)
	if err := os.WriteFile(p, []byte(invalid), 0644); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"import", "validate"} {
		out.Reset()
		if err := run([]string{cmd, p}, &out); err == nil || out.Len() != 0 {
			t.Fatal("structural error emitted graph", cmd, out.String(), err)
		}
	}
}
