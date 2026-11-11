package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cakesmith/meshwork/plugins/subprocess"
)

func TestExecuteSubprocessScenario(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("subprocess fixture requires sh")
	}
	root := t.TempDir()
	graphDir := filepath.Join(root, "meshwork")
	if err := os.Mkdir(graphDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const input = "hello, meshwork\n"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
	for path, content := range map[string]string{
		"input.txt": input,
		"copy.sh":   "cat work/input.txt > output.txt\n",
		"meshwork/graph.copy.meshwork.yaml": `namespace: /copy
places: {incoming: InputFile, outgoing: OutputFile}
transitions: {copy: {implementation: files.copy}}
arcs:
  - {from: incoming, to: copy, port: source}
  - {from: copy, to: outgoing, port: result}
`,
		"meshwork/run.scenario.copy.meshwork.yaml": fmt.Sprintf(`graph: copy
name: verify-copy
marking: {incoming: %q}
expect: {outgoing: %q}
`, digest, digest),
		"meshwork/subprocess.yaml": `root: ..
implementations:
  files.copy:
    argv: [sh, "${ROOT}/copy.sh"]
    inputs: {source: work/input.txt}
    outputs: {result: output.txt}
inputs: {incoming: input.txt}
outputs: {outgoing: result.txt}
`,
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"execute", graphDir, "--scenario", filepath.Join(graphDir, "run.scenario.copy.meshwork.yaml"),
		"--subprocess", filepath.Join(graphDir, "subprocess.yaml")}
	var stdout bytes.Buffer
	if err := run(args, &stdout); err != nil {
		t.Fatal(err)
	}
	var report subprocess.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	result := report.Outputs["/copy/outgoing"]
	if report.Scenario != "verify-copy" || string(result.SHA256) != fmt.Sprintf("%q", digest) || result.TokenHash == "" {
		t.Fatalf("unexpected artifact report %s", &stdout)
	}
	got, err := os.ReadFile(filepath.Join(root, "result.txt"))
	if err != nil || string(got) != input {
		t.Fatalf("published output = %q, %v, want %q", got, err, input)
	}
}
