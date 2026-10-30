package streamexec_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"example.com/meshwork-prototype/linker"
	"example.com/meshwork-prototype/meshwork"
	"example.com/meshwork-prototype/plugins/api"
	"example.com/meshwork-prototype/plugins/remote"
	"example.com/meshwork-prototype/plugins/streamexec"
)

type frames chan []byte

func (f frames) Write(b []byte) (int, error) { f <- append([]byte(nil), b...); return len(b), nil }

func TestRemoteStreamProgressAndBackpressure(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{
		"graph.echo.meshwork.yaml": `namespace: /echo
places: {A: Number, Q: Number}
transitions: {echo: {implementation: echo, timeout: 5s}}
arcs:
  - {from: A, to: echo, port: value, transport: http-json}
  - {from: echo, to: Q, port: result, transport: http-json}
`,
		"run.scenario.echo.meshwork.yaml": "namespace: /echo\ngraph: echo\nname: stream\ninput: stdin\noutput: stdout\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	linked, err := linker.Link([]linker.Repository{{Name: "test", Root: root}})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := remote.ExecutorHandler(map[string]meshwork.Compute{"echo": func(_ context.Context, in map[string]meshwork.Token) (map[string]meshwork.Token, error) {
		return map[string]meshwork.Token{"result": in["value"]}, nil
	}}, "test-secret")
	server := httptest.NewServer(h)
	defer server.Close()
	t.Setenv("MESHWORK_STREAM_TEST_TOKEN", "test-secret")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, err := remote.Prepare(ctx, linked.Graph, remote.Deployment{DefaultExecutor: "worker", Executors: map[string]remote.Executor{"worker": {Type: "http-json", Endpoint: server.URL, TokenEnv: "MESHWORK_STREAM_TEST_TOKEN"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	in, writer := io.Pipe()
	defer in.Close()
	defer writer.Close()
	out := make(frames, 32)
	done := make(chan error, 1)
	go func() {
		done <- (streamexec.Plugin{}).Execute(ctx, api.ExecutionRequest{Graph: linked.Graph, Scenario: linked.Scenarios[0], NewRuntime: a.NewRuntime, Input: in, Output: out})
	}()
	check := func(want int) {
		t.Helper()
		select {
		case raw := <-out:
			var frame struct {
				Place string `json:"place"`
				Value int    `json:"value"`
			}
			if err := json.Unmarshal(raw, &frame); err != nil || frame.Place != "/echo/Q" || frame.Value != want {
				t.Fatalf("want %d got %s (%v)", want, raw, err)
			}
		case err := <-done:
			t.Fatalf("stream ended early: %v", err)
		case <-ctx.Done():
			t.Fatal("stream stalled or dropped input")
		}
	}
	go func() { _, _ = fmt.Fprintln(writer, `{"place":"A","value":0}`) }()
	// The first result must arrive while stdin is still open and without a second input.
	check(0)
	go func() {
		for i := 1; i <= 20; i++ {
			if _, err := fmt.Fprintf(writer, "{\"place\":\"A\",\"value\":%d}\n", i); err != nil {
				return
			}
		}
	}()
	for i := 1; i <= 20; i++ {
		check(i)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream failed to stop")
	}
}
