package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"example.com/meshwork-prototype/meshwork"
	"gopkg.in/yaml.v3"
)

type Executor struct {
	Type     string `yaml:"type"`
	Endpoint string `yaml:"endpoint,omitempty"`
	TokenEnv string `yaml:"token_env,omitempty"`
}

// Deployment binds canonical transition identities, independently of graph sources.
type Deployment struct {
	Authority struct {
		Listen    string `yaml:"listen"`
		Advertise string `yaml:"advertise,omitempty"`
	} `yaml:"authority"`
	DefaultExecutor string              `yaml:"default_executor"`
	Executors       map[string]Executor `yaml:"executors"`
	Transitions     map[string]string   `yaml:"transitions,omitempty"`
}

func LoadDeployment(path string) (Deployment, error) {
	var d Deployment
	b, err := os.ReadFile(path)
	if err != nil {
		return d, err
	}
	if len(b) > MaxMessageBytes {
		return d, fmt.Errorf("deployment is too large")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(b))
	decoder.KnownFields(true)
	if err = decoder.Decode(&d); err != nil {
		return Deployment{}, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Deployment{}, fmt.Errorf("expected one deployment document")
	}
	return d, nil
}

type Authority struct {
	graph     meshwork.Graph
	executors map[string]meshwork.Compute
	server    *http.Server
	base      string
	mu        sync.Mutex
	inputs    map[string]Value
	ctx       context.Context
	cancel    context.CancelFunc
	remote    bool
}

// Prepare validates every selected binding before starting an input server.
// The caller owns Close; the authority is in-memory and has no restart recovery.
func Prepare(ctx context.Context, g meshwork.Graph, d Deployment, native map[string]meshwork.Compute) (*Authority, error) {
	a := &Authority{graph: g, executors: map[string]meshwork.Compute{}, inputs: map[string]Value{}}
	a.ctx, a.cancel = context.WithCancel(ctx)
	ok := false
	defer func() {
		if !ok {
			a.Close()
		}
	}()
	known := map[string]bool{}
	for _, t := range g.Transitions {
		known[t.Name] = true
	}
	for name := range d.Transitions {
		if !known[name] {
			return nil, fmt.Errorf("deployment names unknown transition %s", name)
		}
	}
	if d.DefaultExecutor == "" {
		return nil, fmt.Errorf("default_executor is required")
	}
	if _, exists := d.Executors[d.DefaultExecutor]; !exists {
		return nil, fmt.Errorf("unknown default executor %s", d.DefaultExecutor)
	}
	for _, name := range names(d.Executors) {
		e := d.Executors[name]
		if name == "" || (e.Type != "native" && e.Type != "http-json") {
			return nil, fmt.Errorf("invalid executor %q type %q", name, e.Type)
		}
		if e.Type == "native" && (e.Endpoint != "" || e.TokenEnv != "") {
			return nil, fmt.Errorf("native executor %s cannot have network settings", name)
		}
		if e.Type == "http-json" {
			if err := validBase(e.Endpoint); err != nil {
				return nil, fmt.Errorf("executor %s: %w", name, err)
			}
			if e.TokenEnv == "" {
				return nil, fmt.Errorf("executor %s requires token_env", name)
			}
		}
	}
	caps := map[string]Capabilities{}
	for _, t := range g.Transitions {
		if err := t.ValidateTransports(); err != nil {
			return nil, err
		}
		name := d.DefaultExecutor
		if v, exists := d.Transitions[t.Name]; exists {
			name = v
		}
		e, exists := d.Executors[name]
		if !exists {
			return nil, fmt.Errorf("%s: unknown executor %s", t.Name, name)
		}
		want := "memory"
		if e.Type == "http-json" {
			want = "http-json"
		}
		for direction, ports := range [][]string{t.InputPorts, t.OutputPorts} {
			bindings := t.InputTransports
			if direction == 1 {
				bindings = t.OutputTransports
			}
			for _, p := range ports {
				transport := bindings[p]
				if transport == "" {
					transport = "memory"
				}
				if transport != want {
					return nil, fmt.Errorf("%s port %s: executor %s requires %s transport, got %s", t.Name, p, name, want, transport)
				}
			}
		}
		if e.Type == "native" {
			fn := native[t.Implementation]
			if fn == nil {
				return nil, fmt.Errorf("%s: unknown native implementation %s", t.Name, t.Implementation)
			}
			a.executors[t.Name] = fn
			continue
		}
		if t.Implementation == "meshwork.timeout" || t.Implementation == "meshwork.delay" {
			return nil, fmt.Errorf("%s: remote runtime clock capabilities are unsupported", t.Name)
		}
		secret := os.Getenv(e.TokenEnv)
		if secret == "" {
			return nil, fmt.Errorf("executor %s token environment variable is empty", name)
		}
		capability, exists := caps[name]
		if !exists {
			check, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := exchange(check, http.MethodGet, strings.TrimRight(e.Endpoint, "/")+"/capabilities", secret, nil, &capability)
			cancel()
			if err != nil {
				return nil, fmt.Errorf("executor %s: %w", name, err)
			}
			if capability.Protocol != Protocol || capability.Transport != "http-json" {
				return nil, fmt.Errorf("executor %s: incompatible protocol/transport", name)
			}
			caps[name] = capability
		}
		found := false
		for _, implementation := range capability.Implementations {
			if implementation == t.Implementation {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("executor %s does not export %s", name, t.Implementation)
		}
		a.remote = true
		a.executors[t.Name] = a.bind(t, e.Endpoint, secret)
	}
	// Validate graph structure and complete executor availability before opening a listener.
	if _, err := g.InstantiateExecutors(a.executors, meshwork.RealClock{}); err != nil {
		return nil, err
	}
	if a.remote {
		address := d.Authority.Listen
		if address == "" {
			address = "127.0.0.1:0"
		}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return nil, err
		}
		base := d.Authority.Advertise
		if base == "" {
			host, _, _ := net.SplitHostPort(listener.Addr().String())
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				listener.Close()
				return nil, fmt.Errorf("non-loopback authority requires an advertised URL")
			}
			base = "http://" + listener.Addr().String()
		}
		if err := validBase(base); err != nil {
			listener.Close()
			return nil, fmt.Errorf("authority advertise: %w", err)
		}
		a.base = strings.TrimRight(base, "/")
		a.server = &http.Server{Handler: http.HandlerFunc(a.serveInput), ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = a.server.Serve(listener) }()
	}
	ok = true
	return a, nil
}

func validBase(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("expected an http(s) origin URL")
	}
	return nil
}

func (a *Authority) Close() {
	if a.cancel != nil {
		a.cancel()
	}
	if a.server != nil {
		_ = a.server.Close()
	}
}

func (a *Authority) NewRuntime(clock meshwork.Clock) (*meshwork.Runtime, error) {
	if a.remote {
		if _, ok := clock.(meshwork.RealClock); !ok {
			return nil, fmt.Errorf("remote executors currently require a real clock")
		}
	}
	if err := a.ctx.Err(); err != nil {
		return nil, err
	}
	return a.graph.InstantiateExecutors(a.executors, clock)
}

func (a *Authority) serveInput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.mu.Lock()
	v, ok := a.inputs[r.URL.Path]
	a.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, v)
}

func (a *Authority) bind(t meshwork.AtomicTransition, endpoint, secret string) meshwork.Compute {
	return func(parent context.Context, in map[string]meshwork.Token) (map[string]meshwork.Token, error) {
		ctx, cancel := context.WithCancel(parent)
		defer cancel()
		stop := context.AfterFunc(a.ctx, cancel)
		defer stop()
		var nonce [24]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return nil, err
		}
		id := hex.EncodeToString(nonce[:])
		req := Invocation{Protocol: Protocol, ID: id, Implementation: t.Implementation, Inputs: map[string]string{}}
		var paths []string
		a.mu.Lock()
		for i, p := range names(in) {
			path := fmt.Sprintf("/inputs/%s/%d", id, i)
			paths = append(paths, path)
			token := in[p]
			a.inputs[path] = Value{Type: token.Type, Value: append([]byte(nil), token.Data...)}
			req.Inputs[p] = a.base + path
		}
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			for _, path := range paths {
				delete(a.inputs, path)
			}
		}()
		var response Result
		if err := exchange(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/execute", secret, req, &response); err != nil {
			return nil, err
		}
		if response.Protocol != Protocol || response.ID != id {
			return nil, fmt.Errorf("executor response identity/protocol mismatch")
		}
		if response.Error != "" {
			return nil, fmt.Errorf("executor: %s", response.Error)
		}
		out := map[string]meshwork.Token{}
		for p, v := range response.Outputs {
			token, err := v.token()
			if err != nil {
				return nil, fmt.Errorf("output %s: %w", p, err)
			}
			out[p] = token
		}
		return out, nil
	}
}
