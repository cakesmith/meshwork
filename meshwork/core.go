// Package meshwork is an intentionally small prototype of a 1-safe Petri-net runtime.
package meshwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Token is an immutable, content-addressed typed value. Bytes are copied at boundaries.
type Token struct {
	Type string
	Hash string
	Data []byte
}

func NewToken(typ string, value any) (Token, error) {
	if typ == "" {
		return Token{}, errors.New("token type is required")
	}
	b, err := json.Marshal(value)
	if err != nil {
		return Token{}, err
	}
	// Normalize object ordering even when values arrive as structs or RawMessage.
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return Token{}, err
	}
	b, err = json.Marshal(normalized)
	if err != nil {
		return Token{}, err
	}
	// Domain separation prevents equal bytes of different token types from aliasing.
	hash := sha256.Sum256(append(append([]byte(typ), 0), b...))
	return Token{Type: typ, Hash: hex.EncodeToString(hash[:]), Data: append([]byte(nil), b...)}, nil
}
func (t Token) Clone() Token         { t.Data = append([]byte(nil), t.Data...); return t }
func (t Token) Decode(out any) error { return json.Unmarshal(t.Data, out) }

// Compute is an atomic transition's pure/logical implementation. External effects
// must be handled deliberately by their author; a context deadline cannot undo them.
type Compute func(context.Context, map[string]Token) (map[string]Token, error)
type Transition struct {
	Name    string
	Inputs  []string
	Outputs []string
	Timeout time.Duration // zero means infinite (no Meshwork-imposed deadline)
	Run     Compute
}
type Place struct{ Name, Type string }
type placeState struct {
	definition              Place
	token                   *Token
	inputOwner, outputOwner uint64
}
type Runtime struct {
	mu          sync.Mutex
	places      map[string]*placeState
	transitions map[string]Transition
	attempt     uint64
	clock       Clock
	clocks      map[string]Clock
	active      map[uint64]*Attempt
	changed     chan struct{}
	OnError     func(transition string, err error) // called after reservations release
}

func New() *Runtime {
	return NewWithClock(RealClock{})
}
func NewWithClock(clock Clock) *Runtime {
	if clock == nil {
		clock = RealClock{}
	}
	return &Runtime{places: map[string]*placeState{}, transitions: map[string]Transition{}, clock: clock, clocks: map[string]Clock{"execution": clock}, active: map[uint64]*Attempt{}, changed: make(chan struct{})}
}
func validName(n string) bool {
	if !strings.HasPrefix(n, "/") {
		return false
	}
	for _, part := range strings.Split(n[1:], "/") {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, " \t\r\n\\") {
			return false
		}
	}
	return true
}
func (r *Runtime) AddPlace(p Place) error {
	if !validName(p.Name) || p.Type == "" {
		return fmt.Errorf("invalid place: %+v", p)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.transitions[p.Name]; ok {
		return fmt.Errorf("name used by transition: %s", p.Name)
	}
	if old, ok := r.places[p.Name]; ok {
		if old.definition.Type != p.Type {
			return fmt.Errorf("incompatible place declarations: %s", p.Name)
		}
		return nil
	}
	r.places[p.Name] = &placeState{definition: p}
	return nil
}
func unique(names []string) bool {
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			return false
		}
		seen[n] = true
	}
	return true
}
func (r *Runtime) AddTransition(t Transition) error {
	if !validName(t.Name) || t.Run == nil || len(t.Inputs) == 0 || len(t.Outputs) == 0 || t.Timeout < 0 {
		return fmt.Errorf("invalid transition %s", t.Name)
	}
	if !unique(t.Inputs) || !unique(t.Outputs) {
		return fmt.Errorf("duplicate arc on %s", t.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.places[t.Name]; ok {
		return fmt.Errorf("name used by place: %s", t.Name)
	}
	if _, ok := r.transitions[t.Name]; ok {
		return fmt.Errorf("transition already defined: %s", t.Name)
	}
	for _, p := range append(append([]string(nil), t.Inputs...), t.Outputs...) {
		if _, ok := r.places[p]; !ok {
			return fmt.Errorf("unknown place %s", p)
		}
	}
	t.Inputs = append([]string(nil), t.Inputs...)
	t.Outputs = append([]string(nil), t.Outputs...)
	r.transitions[t.Name] = t
	return nil
}

// Put seeds an empty place; seed operations are external initial-marking setup.
func (r *Runtime) Put(name string, t Token) error {
	if err := validateToken(t); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.places[name]
	if !ok {
		return fmt.Errorf("unknown place %s", name)
	}
	if t.Type != p.definition.Type {
		return fmt.Errorf("place %s expects %s, got %s", name, p.definition.Type, t.Type)
	}
	if p.token != nil || p.outputOwner != 0 || p.inputOwner != 0 {
		return fmt.Errorf("place unavailable %s", name)
	}
	cp := t.Clone()
	p.token = &cp
	return nil
}

type Snapshot struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	State string `json:"state"`
	Hash  string `json:"hash,omitempty"`
}

// Token returns an isolated copy of an actual token, regardless of lock metadata.
func (r *Runtime) Token(name string) (Token, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.places[name]
	if !ok || p.token == nil {
		return Token{}, false
	}
	return p.token.Clone(), true
}

func (r *Runtime) Snapshot() []Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []Snapshot
	for name, p := range r.places {
		s := Snapshot{Name: name, Type: p.definition.Type, State: "empty"}
		if p.token != nil {
			s.State = "full"
			s.Hash = p.token.Hash
		}
		if p.outputOwner != 0 {
			s.State += "+output-locked"
		}
		if p.inputOwner != 0 {
			s.State += "+input-reserved"
		}
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

var ErrNotEnabled = errors.New("transition not enabled")

// Fire starts one attempt and waits for its atomic commit or finalized failure.
func (r *Runtime) Fire(parent context.Context, name string) error {
	a, err := r.Start(parent, name)
	if err != nil {
		return err
	}
	return a.Wait(parent)
}

// Start reserves all inputs/outputs before returning a unique execution attempt.
func (r *Runtime) Start(parent context.Context, name string) (*Attempt, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	t, ok := r.transitions[name]
	if !ok {
		r.mu.Unlock()
		return nil, fmt.Errorf("unknown transition %s", name)
	}
	for _, n := range t.Inputs {
		p := r.places[n]
		if p.token == nil || p.inputOwner != 0 {
			r.mu.Unlock()
			return nil, fmt.Errorf("%w: input %s", ErrNotEnabled, n)
		}
	}
	for _, n := range t.Outputs {
		p := r.places[n]
		if p.outputOwner != 0 {
			r.mu.Unlock()
			return nil, fmt.Errorf("%w: output locked %s", ErrNotEnabled, n)
		}
		// An input/output self-loop consumes and replaces its own token.
		isInput := false
		for _, in := range t.Inputs {
			if in == n {
				isInput = true
				break
			}
		}
		if p.token != nil && !isInput {
			r.mu.Unlock()
			return nil, fmt.Errorf("%w: output full %s", ErrNotEnabled, n)
		}
	}
	r.attempt++
	id := r.attempt
	inputs := make(map[string]Token, len(t.Inputs))
	for _, n := range t.Inputs {
		p := r.places[n]
		p.inputOwner = id
		inputs[n] = p.token.Clone()
	}
	for _, n := range t.Outputs {
		r.places[n].outputOwner = id
	}

	ctx, cancel := context.WithCancel(parent)
	a := &Attempt{id: id, runtime: r, transition: t, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	a.ctx = context.WithValue(ctx, attemptKey{}, a)
	if t.Timeout > 0 {
		a.deadline = r.clock.Now().Add(t.Timeout)
		a.timer = r.clock.NewTimer(t.Timeout)
	}
	r.active[id] = a
	r.notifyLocked()
	r.mu.Unlock()
	go a.execute(inputs)
	go func() {
		var deadline <-chan time.Time
		if a.timer != nil {
			deadline = a.timer.C()
		}
		select {
		case <-deadline:
			r.finish(a, nil, context.DeadlineExceeded)
		case <-parent.Done():
			r.finish(a, nil, parent.Err())
		case <-a.done:
		}
	}()
	return a, nil
}

func validateToken(t Token) error {
	if !json.Valid(t.Data) {
		return errors.New("invalid token JSON")
	}
	canonical, err := NewToken(t.Type, json.RawMessage(t.Data))
	if err != nil {
		return err
	}
	if t.Hash != canonical.Hash || !bytes.Equal(t.Data, canonical.Data) {
		return errors.New("token content or hash is not canonical")
	}
	return nil
}
