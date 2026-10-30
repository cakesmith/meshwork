// Package streamexec executes persistent stdin/stdout scenarios.
package streamexec

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"example.com/meshwork-prototype/linker"
	"example.com/meshwork-prototype/meshwork"
	"example.com/meshwork-prototype/plugins/api"
	"fmt"
	"io"
	"sort"
)

type Plugin struct{}

func (Plugin) Name() string { return "stream" }
func (Plugin) Enabled(s linker.Scenario) bool {
	return !api.HasExpectations(s)
}
func (Plugin) Execute(ctx context.Context, request api.ExecutionRequest) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := request.Scenario
	if s.Input != "stdin" || s.Output != "stdout" {
		return fmt.Errorf("scenario %s is not a streaming scenario", s.Name)
	}
	r, err := request.Instantiate(meshwork.RealClock{})
	if err != nil {
		return err
	}
	ingress := map[string]bool{}
	for _, n := range s.Ingress {
		ingress[n] = true
	}
	types := map[string]string{}
	for _, p := range request.Graph.Places {
		types[p.Name] = p.Type
	}
	input := make(chan streamMessage)
	readErr := make(chan error, 1)
	go readStream(ctx, request.Input, input, readErr)
	encoder := json.NewEncoder(request.Output)
	transitions := append([]meshwork.AtomicTransition(nil), request.Graph.Transitions...)
	sort.Slice(transitions, func(i, j int) bool { return transitions[i].Name < transitions[j].Name })
	done := make(chan error, len(transitions))
	var pending *streamMessage
	for {
		// Observe before scanning so a concurrent completion cannot be missed.
		changed := r.Changed()
		for _, t := range transitions {
			a, err := r.Start(ctx, t.Name)
			if errors.Is(err, meshwork.ErrNotEnabled) {
				continue
			}
			if err != nil {
				return err
			}
			go func() {
				err := a.Wait(ctx)
				select {
				case done <- err:
				case <-ctx.Done():
				}
			}()
		}
		for _, n := range s.Egress {
			tok, ok := r.Token(n)
			if !ok {
				continue
			}
			if err := encoder.Encode(streamMessage{Place: n, Value: tok.Data}); err != nil {
				return err
			}
			if _, ok, err := r.Take(n); err != nil {
				return fmt.Errorf("stream output %s: %w", n, err)
			} else if !ok {
				return fmt.Errorf("stream output %s disappeared", n)
			}
		}
		if pending != nil {
			name, err := s.ResolveInput(pending.Place)
			if err != nil {
				return err
			}
			if !ingress[name] {
				return fmt.Errorf("stream stdin references non-input place %s", pending.Place)
			}
			tok, err := meshwork.NewToken(types[name], json.RawMessage(pending.Value))
			if err != nil {
				return fmt.Errorf("stream stdin place %s: %w", pending.Place, err)
			}
			if err := r.Put(name, tok); err == nil {
				pending = nil
				continue
			} else if !errors.Is(err, meshwork.ErrPlaceUnavailable) {
				return err
			}
		}
		nextInput := input
		if pending != nil {
			nextInput = nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
		case err := <-readErr:
			if err != nil {
				return err
			}
			readErr = nil
		case m, ok := <-nextInput:
			if ok {
				pending = &m
			} else {
				input = nil
			}
		case <-changed:
		}
	}
}

type streamMessage struct {
	Place string          `json:"place"`
	Value json.RawMessage `json:"value"`
}

func readStream(ctx context.Context, in io.Reader, input chan<- streamMessage, readErr chan<- error) {
	defer close(input)
	d := json.NewDecoder(bufio.NewReader(in))
	for {
		var m streamMessage
		if err := d.Decode(&m); err == io.EOF {
			readErr <- nil
			return
		} else if err != nil {
			readErr <- fmt.Errorf("stream stdin: %w", err)
			return
		}
		if m.Place == "" || !json.Valid(m.Value) {
			readErr <- errors.New("stream stdin requires place and JSON value")
			return
		}
		select {
		case input <- m:
		case <-ctx.Done():
			return
		}
	}
}
