// Package remote binds transition executors over the meshwork.executor.v1 protocol.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"example.com/meshwork-prototype/meshwork"
)

const Protocol = "meshwork.executor.v1"
const MaxMessageBytes = 4 << 20

type Value struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

func (v Value) token() (meshwork.Token, error) {
	if !json.Valid(v.Value) {
		return meshwork.Token{}, fmt.Errorf("missing or invalid token value")
	}
	return meshwork.NewToken(v.Type, v.Value)
}

type Capabilities struct {
	Protocol        string   `json:"protocol"`
	Transport       string   `json:"transport"`
	Implementations []string `json:"implementations"`
}

type Invocation struct {
	Protocol       string            `json:"protocol"`
	ID             string            `json:"id"`
	Implementation string            `json:"implementation"`
	Inputs         map[string]string `json:"inputs"`
}

type Result struct {
	Protocol string           `json:"protocol"`
	ID       string           `json:"id"`
	Outputs  map[string]Value `json:"outputs,omitempty"`
	Error    string           `json:"error,omitempty"`
}

func decode(r io.Reader, out any) error {
	b, err := io.ReadAll(io.LimitReader(r, MaxMessageBytes+1))
	if err != nil {
		return err
	}
	if len(b) > MaxMessageBytes {
		return fmt.Errorf("protocol message exceeds %d bytes", MaxMessageBytes)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(out); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}

var client = &http.Client{
	Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 0, IdleConnTimeout: 30 * time.Second},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("executor protocol redirects are not supported")
	},
}

func exchange(ctx context.Context, method, url, secret string, input, output any) error {
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		if len(b) > MaxMessageBytes {
			return fmt.Errorf("protocol request too large")
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("invalid protocol endpoint")
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("executor transport failed: %w", withoutURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("executor protocol HTTP status %d", resp.StatusCode)
	}
	return decode(resp.Body, output)
}

// Capability URLs must never leak through a network error's request URL.
func withoutURL(err error) error {
	if e, ok := err.(interface{ Unwrap() error }); ok {
		return e.Unwrap()
	}
	return err
}

func writeJSON(w http.ResponseWriter, value any) {
	b, err := json.Marshal(value)
	if err != nil || len(b) > MaxMessageBytes {
		http.Error(w, "protocol response too large or invalid", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func names[V any](m map[string]V) []string {
	n := make([]string, 0, len(m))
	for k := range m {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}
