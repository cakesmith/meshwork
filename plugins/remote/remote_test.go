package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/meshwork-prototype/meshwork"
)

func testGraph() meshwork.Graph {
	return meshwork.Graph{Places: []meshwork.Place{{Name: "/A", Type: "Bool"}, {Name: "/B", Type: "Bool"}, {Name: "/Q", Type: "Bool"}}, Transitions: []meshwork.AtomicTransition{{
		Name: "/work", Implementation: "effect", Inputs: []string{"/A", "/B"}, Outputs: []string{"/Q"}, InputPorts: []string{"left", "right"}, OutputPorts: []string{"result"},
		InputTransports: map[string]string{"left": "http-json", "right": "http-json"}, OutputTransports: map[string]string{"result": "http-json"}, Timeout: time.Second,
	}}}
}

func startAuthority(t *testing.T, g meshwork.Graph, fn meshwork.Compute) (*Authority, *meshwork.Runtime) {
	t.Helper()
	h, err := ExecutorHandler(map[string]meshwork.Compute{"effect": fn}, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	a := prepareTest(t, g, server.URL, nil, nil)
	r, err := a.NewRuntime(meshwork.RealClock{})
	if err != nil {
		t.Fatal(err)
	}
	token, _ := meshwork.NewToken("Bool", true)
	for _, p := range []string{"/A", "/B"} {
		if err := r.Put(p, token); err != nil {
			t.Fatal(err)
		}
	}
	return a, r
}

func prepareTest(t *testing.T, g meshwork.Graph, endpoint string, overrides map[string]string, native map[string]meshwork.Compute) *Authority {
	t.Helper()
	t.Setenv("MESHWORK_TEST_TOKEN", "test-secret")
	d := Deployment{DefaultExecutor: "remote", Executors: map[string]Executor{"remote": {Type: "http-json", Endpoint: endpoint, TokenEnv: "MESHWORK_TEST_TOKEN"}, "local": {Type: "native"}}, Transitions: overrides}
	a, err := Prepare(context.Background(), g, d, native)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

func TestRemoteAndNativePortsShareOnePlace(t *testing.T) {
	g := testGraph()
	g.Places = append(g.Places, meshwork.Place{Name: "/done", Type: "Bool"})
	g.Transitions = append(g.Transitions, meshwork.AtomicTransition{Name: "/local", Implementation: "effect", Inputs: []string{"/Q"}, Outputs: []string{"/done"}, InputPorts: []string{"value"}, OutputPorts: []string{"result"}})
	var remoteCalls atomic.Int32
	h, _ := ExecutorHandler(map[string]meshwork.Compute{"effect": func(_ context.Context, in map[string]meshwork.Token) (map[string]meshwork.Token, error) {
		remoteCalls.Add(1)
		return map[string]meshwork.Token{"result": in["left"]}, nil
	}}, "test-secret")
	server := httptest.NewServer(h)
	defer server.Close()
	a := prepareTest(t, g, server.URL, map[string]string{"/local": "local"}, map[string]meshwork.Compute{"effect": func(_ context.Context, in map[string]meshwork.Token) (map[string]meshwork.Token, error) {
		return map[string]meshwork.Token{"result": in["value"]}, nil
	}})
	r, err := a.NewRuntime(meshwork.RealClock{})
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := meshwork.NewToken("Bool", true)
	_ = r.Put("/A", tok)
	_ = r.Put("/B", tok)
	if err = r.Fire(context.Background(), "/work"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Token("/Q"); !ok {
		t.Fatal("remote result not committed into shared place")
	}
	if err = r.Fire(context.Background(), "/local"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Token("/Q"); ok {
		t.Fatal("native consumer failed to consume shared place")
	}
	if out, ok := r.Token("/done"); !ok || out.Hash != tok.Hash || remoteCalls.Load() != 1 {
		t.Fatalf("wrong result %+v calls %d", out, remoteCalls.Load())
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.inputs) != 0 {
		t.Fatal("input capabilities leaked")
	}
}

func TestRemoteFailureLeavesCompleteMarkingUnchanged(t *testing.T) {
	for _, mode := range []string{"error", "wrong-type", "wrong-port", "malformed-value", "panic"} {
		t.Run(mode, func(t *testing.T) {
			_, r := startAuthority(t, testGraph(), func(_ context.Context, in map[string]meshwork.Token) (map[string]meshwork.Token, error) {
				switch mode {
				case "error":
					return nil, fmt.Errorf("failed")
				case "panic":
					panic("failed")
				case "wrong-type":
					v, _ := meshwork.NewToken("Other", true)
					return map[string]meshwork.Token{"result": v}, nil
				case "wrong-port":
					return map[string]meshwork.Token{"other": in["left"]}, nil
				default:
					return map[string]meshwork.Token{"result": {Type: "Bool", Data: []byte("invalid")}}, nil
				}
			})
			before := r.Snapshot()
			if err := r.Fire(context.Background(), "/work"); err == nil {
				t.Fatal("accepted invalid effect result")
			}
			if after := r.Snapshot(); !reflect.DeepEqual(before, after) {
				t.Fatalf("partial state: before %+v after %+v", before, after)
			}
		})
	}
}

func TestRemoteCancellationRejectsLateResult(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var calls atomic.Int32
	g := testGraph()
	g.Transitions[0].Timeout = 0
	_, r := startAuthority(t, g, func(_ context.Context, in map[string]meshwork.Token) (map[string]meshwork.Token, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			defer close(finished)
		}
		return map[string]meshwork.Token{"result": in["left"]}, nil
	})
	var released sync.Once
	unblock := func() { released.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	before := r.Snapshot()
	a, err := r.Start(ctx, "/work")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("executor did not start")
	}
	if _, err = r.Start(ctx, "/work"); err == nil {
		t.Fatal("competing attempt acquired reserved inputs")
	}
	cancel()
	if err = a.Wait(context.Background()); err == nil {
		t.Fatal("canceled attempt succeeded")
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatal("cancellation did not restore full state")
	}
	// A new attempt can commit before the stale executor returns.
	retry, stopRetry := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopRetry()
	if err = r.Fire(retry, "/work"); err != nil {
		t.Fatal(err)
	}
	committed := r.Snapshot()
	unblock()
	<-finished
	if !reflect.DeepEqual(committed, r.Snapshot()) {
		t.Fatal("late output changed newer state")
	}
}

func TestRemoteBindingAndWireValidation(t *testing.T) {
	for _, mode := range []string{"wrong-id", "wrong-version", "extra-field", "disconnected"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/capabilities" {
					writeJSON(w, Capabilities{Protocol: Protocol, Transport: "http-json", Implementations: []string{"effect"}})
					return
				}
				var request Invocation
				_ = json.NewDecoder(r.Body).Decode(&request)
				switch mode {
				case "wrong-id":
					writeJSON(w, Result{Protocol: Protocol, ID: "stale"})
				case "wrong-version":
					writeJSON(w, Result{Protocol: "other", ID: request.ID})
				case "extra-field":
					_, _ = w.Write([]byte(`{"unknown":true}`))
				case "disconnected":
					connection, _, _ := w.(http.Hijacker).Hijack()
					_ = connection.Close()
				}
			}))
			defer server.Close()
			a := prepareTest(t, testGraph(), server.URL, nil, nil)
			r, _ := a.NewRuntime(meshwork.RealClock{})
			v, _ := meshwork.NewToken("Bool", true)
			_ = r.Put("/A", v)
			_ = r.Put("/B", v)
			before := r.Snapshot()
			if err := r.Fire(context.Background(), "/work"); err == nil {
				t.Fatal("accepted bad protocol")
			}
			if !reflect.DeepEqual(before, r.Snapshot()) {
				t.Fatal("bad protocol changed state")
			}
			if _, err := a.NewRuntime(meshwork.NewManualClock(time.Unix(0, 0))); err == nil {
				t.Fatal("accepted unsupported manual clock")
			}
		})
	}
	var v Value
	if err := decode(strings.NewReader(strings.Repeat(" ", MaxMessageBytes+1)), &v); err == nil {
		t.Fatal("accepted oversized message")
	}
	if err := decode(strings.NewReader(`{"type":"Bool","value":true} {}`), &v); err == nil {
		t.Fatal("accepted trailing data")
	}
}

func TestRemotePartialOutputCannotCommit(t *testing.T) {
	g := testGraph()
	g.Places = append(g.Places, meshwork.Place{Name: "/extra", Type: "Bool"})
	g.Transitions[0].Outputs = append(g.Transitions[0].Outputs, "/extra")
	g.Transitions[0].OutputPorts = append(g.Transitions[0].OutputPorts, "extra")
	g.Transitions[0].OutputTransports["extra"] = "http-json"
	_, r := startAuthority(t, g, func(_ context.Context, in map[string]meshwork.Token) (map[string]meshwork.Token, error) {
		bad, _ := meshwork.NewToken("Other", true)
		return map[string]meshwork.Token{"result": in["left"], "extra": bad}, nil
	})
	before := r.Snapshot()
	if err := r.Fire(context.Background(), "/work"); err == nil {
		t.Fatal("accepted invalid second output")
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatal("part of an invalid output set was committed")
	}
}

func TestRemoteTransportAndBindingRejections(t *testing.T) {
	t.Setenv("MESHWORK_TEST_TOKEN", "test-secret")
	h, _ := ExecutorHandler(map[string]meshwork.Compute{"effect": func(context.Context, map[string]meshwork.Token) (map[string]meshwork.Token, error) {
		t.Error("invalid binding executed")
		return nil, nil
	}}, "test-secret")
	s := httptest.NewServer(h)
	defer s.Close()
	for _, mode := range []string{"unknown-transition", "missing-export", "incompatible-arc", "unknown-executor", "capability-effect"} {
		t.Run(mode, func(t *testing.T) {
			g := testGraph()
			d := Deployment{DefaultExecutor: "worker", Executors: map[string]Executor{"worker": {Type: "http-json", Endpoint: s.URL, TokenEnv: "MESHWORK_TEST_TOKEN"}}}
			switch mode {
			case "unknown-transition":
				d.Transitions = map[string]string{"/missing": "worker"}
			case "missing-export":
				g.Transitions[0].Implementation = "missing"
			case "incompatible-arc":
				delete(g.Transitions[0].InputTransports, "left")
			case "unknown-executor":
				d.Transitions = map[string]string{"/work": "missing"}
			case "capability-effect":
				g.Transitions[0].Implementation = "meshwork.delay"
			}
			a, err := Prepare(context.Background(), g, d, nil)
			if a != nil {
				a.Close()
			}
			if err == nil || a != nil {
				t.Fatalf("accepted %s: %v", mode, err)
			}
		})
	}
}
