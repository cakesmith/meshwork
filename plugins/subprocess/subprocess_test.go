package subprocess

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cakesmith/meshwork/meshwork"
)

func TestFailedProcessKeepsInputForRetry(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("retry fixture requires sh")
	}
	store := artifactStore(t.TempDir())
	dir := t.TempDir()
	source := filepath.Join(dir, "source.txt")
	ready := filepath.Join(dir, "ready")
	const input = "original input\n"
	if err := os.WriteFile(source, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := store.capture(source, "InputFile")
	if err != nil {
		t.Fatal(err)
	}
	argv := []string{shell, "-c", `
		test ! -e output.txt || exit 99
		if test ! -e "$1"; then
			printf 'partial\n' > output.txt
			exit 23
		fi
		cat input.txt > output.txt
	`, "copy", ready}
	compute := bindProcess(store, io.Discard, argv,
		map[string]string{"source": "input.txt"},
		map[string]outputFile{"result": {path: "output.txt", tokenType: "OutputFile"}})
	graph := meshwork.Graph{
		Places: []meshwork.Place{{Name: "/in", Type: "InputFile"}, {Name: "/out", Type: "OutputFile"}},
		Transitions: []meshwork.AtomicTransition{{
			Name: "/copy", Implementation: "copy", Inputs: []string{"/in"}, Outputs: []string{"/out"},
			InputPorts: []string{"source"}, OutputPorts: []string{"result"},
		}},
	}
	runtime, err := graph.Instantiate(map[string]meshwork.Compute{"copy": compute})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Put("/in", token); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var exit *exec.ExitError
	if err := runtime.Fire(ctx, "/copy"); !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("expected subprocess exit 23, got %v", err)
	}
	if kept, ok := runtime.Token("/in"); !ok || kept.Hash != token.Hash {
		t.Fatal("failed firing changed the input marking")
	}
	if _, ok := runtime.Token("/out"); ok {
		t.Fatal("failed firing committed an output token")
	}
	if err := os.WriteFile(source, []byte("changed after capture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Fire(ctx, "/copy"); err != nil {
		t.Fatal(err)
	}
	if _, ok := runtime.Token("/in"); ok {
		t.Fatal("successful retry did not consume the input")
	}
	result, ok := runtime.Token("/out")
	if !ok || result.Type != "OutputFile" {
		t.Fatal("successful retry did not commit the expected output type")
	}
	dest := filepath.Join(dir, "result.txt")
	if err := store.materialize(result, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != input {
		t.Fatalf("retry output = %q, %v, want captured bytes %q", got, err, input)
	}
	if err := runtime.Put("/in", token); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Fire(ctx, "/copy"); !errors.Is(err, meshwork.ErrNotEnabled) {
		t.Fatalf("expected an occupied output to block firing, got %v", err)
	}
}

func TestSuccessfulProcessRequiresDeclaredOutput(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("process fixture requires sh")
	}
	compute := bindProcess(artifactStore(t.TempDir()), io.Discard, []string{shell, "-c", "exit 0"}, nil,
		map[string]outputFile{"result": {path: "output.txt", tokenType: "OutputFile"}})
	outputs, err := compute(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "capture output result") || outputs != nil {
		t.Fatalf("expected missing-output error, got %v, %v", outputs, err)
	}
}
