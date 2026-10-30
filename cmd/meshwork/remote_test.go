package main

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	distributed "example.com/meshwork-prototype/examples/distributed"
	"example.com/meshwork-prototype/plugins/remote"
)

func TestExecuteRemoteScenario(t *testing.T) {
	t.Setenv("MESHWORK_EXECUTOR_TEST_TOKEN", "test-token")
	handler, err := remote.ExecutorHandler(distributed.Implementations(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	path := filepath.Join(t.TempDir(), "deployment.yaml")
	config := fmt.Sprintf("default_executor: worker\nexecutors:\n  worker:\n    type: http-json\n    endpoint: %s\n    token_env: MESHWORK_EXECUTOR_TEST_TOKEN\n", server.URL)
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	args := []string{"execute", "--scenario", "../../examples/remote/tests.scenarios.nand.meshwork.yaml", "--deployment", path, "../../examples/remote"}
	if err := runInput(args, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "passed:") != 4 {
		t.Fatalf("output: %s", out.String())
	}
	t.Setenv("MESHWORK_EXECUTOR_TEST_TOKEN", "wrong-token")
	out.Reset()
	if err := runInput(args, strings.NewReader(""), &out); err == nil || out.Len() != 0 {
		t.Fatalf("unauthorized executor: %v %s", err, out.String())
	}
}
