package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"example.com/meshwork-prototype/linker"
)

func TestDiffCommands(t *testing.T) {
	fixture := "../../linker/testdata/graphml/graph.nand.graphml"
	for _, args := range [][]string{
		{"diff", fixture, fixture},
		{"diff", fixture, fixture, "--format", "json"},
		{"diff", "--format=text", fixture, fixture},
		{"diff", "../../linker/testdata/graphml", "../../linker/testdata/graphml", "--format", "json"},
	} {
		var out bytes.Buffer
		if e := run(args, &out); e != nil {
			t.Fatal(args, e)
		}
		if strings.Contains(strings.Join(args, " "), "json") {
			var r linker.Report
			if e := json.Unmarshal(out.Bytes(), &r); e != nil {
				t.Fatal(e)
			}
			if len(r.SemanticChanges)+len(r.ProvenanceChanges)+len(r.PossibleRenames) != 0 {
				t.Fatal(out.String())
			}
		} else if !strings.Contains(out.String(), "No semantic changes.") {
			t.Fatal(out.String())
		}
	}
	// Independently named workspace roots must use the same computational context.
	before, after := t.TempDir(), t.TempDir()
	for _, root := range []string{before, after} {
		if e := os.WriteFile(filepath.Join(root, "graph.a.meshwork.yaml"), []byte("places: {A: Bool}\ntransitions: {t: {}}\n"), 0644); e != nil {
			t.Fatal(e)
		}
	}
	var out bytes.Buffer
	if e := run([]string{"diff", before, after, "--format", "json"}, &out); e != nil {
		t.Fatal(e)
	}
	var r linker.Report
	if e := json.Unmarshal(out.Bytes(), &r); e != nil {
		t.Fatal(e)
	}
	if len(r.SemanticChanges) != 0 || len(r.ProvenanceChanges) != 2 {
		t.Fatal(out.String())
	}
	// A real change includes typed values and source locations; repetitions are byte-identical.
	if e := os.WriteFile(filepath.Join(after, "graph.a.meshwork.yaml"), []byte("places: {A: Bit}\ntransitions: {t: {}}\n"), 0644); e != nil {
		t.Fatal(e)
	}
	var expected []byte
	for i := 0; i < 3; i++ {
		out.Reset()
		if e := run([]string{"diff", before, after, "--format", "json"}, &out); e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			expected = append([]byte{}, out.Bytes()...)
		} else if !bytes.Equal(expected, out.Bytes()) {
			t.Fatal("unstable CLI JSON")
		}
	}
	if e := json.Unmarshal(expected, &r); e != nil {
		t.Fatal(e)
	}
	if len(r.SemanticChanges) != 1 || r.SemanticChanges[0].Before.Type != "Bool" || r.SemanticChanges[0].After.Type != "Bit" || len(r.SemanticChanges[0].BeforeSources) != 1 {
		t.Fatal(string(expected))
	}
	for _, args := range [][]string{{"diff"}, {"diff", fixture}, {"diff", fixture, fixture, "extra"}, {"diff", fixture, fixture, "--format", "yaml"}, {"diff", fixture, fixture, "--unknown"}, {"diff", "missing", fixture}} {
		out.Reset()
		if e := run(args, &out); e == nil || out.Len() != 0 {
			t.Fatal("accepted invalid arguments", args)
		}
	}
	if e := os.WriteFile(filepath.Join(after, "graph.a.meshwork.yaml"), []byte("places: {A: Bool, B: Bool}\narcs: [[A,B]]\n"), 0644); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e := run([]string{"diff", before, after}, &out); e == nil || out.Len() != 0 {
		t.Fatal("invalid graph emitted report", out.String())
	}
}
