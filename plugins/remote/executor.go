package remote

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"example.com/meshwork-prototype/meshwork"
)

// ExecutorHandler serves compiled effects without importing any application graph.
// The host supplies the effects explicitly. Runtime clock/capability callbacks are
// not available in v1. Run behind HTTPS or on a trusted private network.
func ExecutorHandler(implementations map[string]meshwork.Compute, secret string) (http.Handler, error) {
	if secret == "" {
		return nil, fmt.Errorf("executor authorization token is required")
	}
	registry := map[string]meshwork.Compute{}
	for name, fn := range implementations {
		if name == "" || fn == nil {
			return nil, fmt.Errorf("invalid executor export %q", name)
		}
		registry[name] = fn
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /capabilities", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, Capabilities{Protocol: Protocol, Transport: "http-json", Implementations: names(registry)})
	})
	mux.HandleFunc("POST /execute", func(w http.ResponseWriter, r *http.Request) {
		var req Invocation
		if err := decode(r.Body, &req); err != nil {
			http.Error(w, "invalid invocation", http.StatusBadRequest)
			return
		}
		result := Result{Protocol: Protocol, ID: req.ID}
		defer func() {
			if recover() != nil {
				result.Outputs = nil
				result.Error = "executor panicked"
			}
			writeJSON(w, result)
		}()
		if req.Protocol != Protocol || req.ID == "" || len(req.Inputs) == 0 {
			result.Error = "unsupported protocol or invalid invocation"
			return
		}
		fn := registry[req.Implementation]
		if fn == nil {
			result.Error = "unknown implementation"
			return
		}
		in := map[string]meshwork.Token{}
		for _, port := range names(req.Inputs) {
			endpoint := req.Inputs[port]
			u, err := url.Parse(endpoint)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/inputs/") {
				result.Error = "invalid input capability endpoint"
				return
			}
			var v Value
			if err := exchange(r.Context(), http.MethodGet, endpoint, "", nil, &v); err != nil {
				result.Error = fmt.Sprintf("input %s: %v", port, err)
				return
			}
			token, err := v.token()
			if err != nil {
				result.Error = err.Error()
				return
			}
			in[port] = token
		}
		out, err := fn(r.Context(), in)
		if err != nil {
			result.Error = err.Error()
			return
		}
		result.Outputs = map[string]Value{}
		for port, token := range out {
			result.Outputs[port] = Value{Type: token.Type, Value: token.Data}
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}), nil
}
