package meshwork

import (
	"context"
	"errors"
	"testing"
	"time"
)

func basic(t *testing.T) *Runtime {
	t.Helper()
	r := New()
	for _, p := range []Place{{"/A", "Bool"}, {"/B", "Bool"}, {"/Q", "Bool"}} {
		if e := r.AddPlace(p); e != nil {
			t.Fatal(e)
		}
	}
	for _, n := range []string{"/A", "/B"} {
		tok, _ := NewToken("Bool", true)
		if e := r.Put(n, tok); e != nil {
			t.Fatal(e)
		}
	}
	return r
}
func TestNAND(t *testing.T) {
	r := basic(t)
	e := r.AddTransition(Transition{Name: "/nand", Inputs: []string{"/A", "/B"}, Outputs: []string{"/Q"}, Run: func(_ context.Context, _ map[string]Token) (map[string]Token, error) {
		q, _ := NewToken("Bool", false)
		return map[string]Token{"/Q": q}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Fire(context.Background(), "/nand"); e != nil {
		t.Fatal(e)
	}
	s := r.Snapshot()
	if s[0].State != "empty" || s[1].State != "empty" || s[2].State != "full" {
		t.Fatalf("bad marking: %+v", s)
	}
}
func TestFailureRestoresMarking(t *testing.T) {
	r := basic(t)
	_ = r.AddTransition(Transition{Name: "/fail", Inputs: []string{"/A"}, Outputs: []string{"/Q"}, Run: func(context.Context, map[string]Token) (map[string]Token, error) { return nil, errors.New("oops") }})
	if e := r.Fire(context.Background(), "/fail"); e == nil {
		t.Fatal("expected failure")
	}
	s := r.Snapshot()
	if s[0].State != "full" || s[2].State != "empty" {
		t.Fatalf("unexpected marking: %+v", s)
	}
}
func TestOutputLockBlocksProducersNotConsumers(t *testing.T) {
	r := basic(t)
	started := make(chan struct{})
	release := make(chan struct{})
	_ = r.AddTransition(Transition{Name: "/slow", Inputs: []string{"/A"}, Outputs: []string{"/Q"}, Run: func(context.Context, map[string]Token) (map[string]Token, error) {
		close(started)
		<-release
		q, _ := NewToken("Bool", true)
		return map[string]Token{"/Q": q}, nil
	}})
	_ = r.AddTransition(Transition{Name: "/other", Inputs: []string{"/B"}, Outputs: []string{"/Q"}, Run: func(context.Context, map[string]Token) (map[string]Token, error) { return nil, nil }})
	done := make(chan error, 1)
	go func() { done <- r.Fire(context.Background(), "/slow") }()
	<-started
	if !errors.Is(r.Fire(context.Background(), "/other"), ErrNotEnabled) {
		t.Fatal("output lock should prevent producer")
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
func TestDeadlineRejectsLateCompletion(t *testing.T) {
	r := basic(t)
	_ = r.AddTransition(Transition{Name: "/slow", Inputs: []string{"/A"}, Outputs: []string{"/Q"}, Timeout: time.Millisecond, Run: func(context.Context, map[string]Token) (map[string]Token, error) {
		time.Sleep(4 * time.Millisecond)
		q, _ := NewToken("Bool", true)
		return map[string]Token{"/Q": q}, nil
	}})
	if e := r.Fire(context.Background(), "/slow"); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", e)
	}
	s := r.Snapshot()
	if s[0].State != "full" || s[2].State != "empty" {
		t.Fatalf("late commit: %+v", s)
	}
}
func TestSameNameSamePlace(t *testing.T) {
	r := New()
	if e := r.AddPlace(Place{"/shared", "Bool"}); e != nil {
		t.Fatal(e)
	}
	if e := r.AddPlace(Place{"/shared", "Bool"}); e != nil {
		t.Fatal(e)
	}
	if e := r.AddPlace(Place{"/shared", "Image"}); e == nil {
		t.Fatal("expected type conflict")
	}
}

func TestSelfLoopAndTokenImmutability(t *testing.T) {
	r := basic(t)
	tok, _ := r.Token("/A")
	tok.Data[0] = 'x'
	original, _ := r.Token("/A")
	if string(original.Data) != "true" {
		t.Fatal("token alias")
	}
	err := r.AddTransition(Transition{Name: "/loop", Inputs: []string{"/A"}, Outputs: []string{"/A"}, Run: func(_ context.Context, in map[string]Token) (map[string]Token, error) { return in, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Fire(context.Background(), "/loop"); err != nil {
		t.Fatal(err)
	}
	if err := r.Put("/A", original); err == nil {
		t.Fatal("overwrote full place")
	}
	if err := r.Put("/Q", tok); err == nil {
		t.Fatal("accepted tampered token")
	}
}

func TestPanicAndInvalidOutputsReleaseReservations(t *testing.T) {
	for _, fn := range []Compute{
		func(context.Context, map[string]Token) (map[string]Token, error) { panic("broken") },
		func(context.Context, map[string]Token) (map[string]Token, error) { return nil, nil },
		func(context.Context, map[string]Token) (map[string]Token, error) {
			tok, _ := NewToken("Image", true)
			return map[string]Token{"/Q": tok}, nil
		},
		func(context.Context, map[string]Token) (map[string]Token, error) {
			tok, _ := NewToken("Bool", true)
			tok.Hash = "fake"
			return map[string]Token{"/Q": tok}, nil
		},
	} {
		r := basic(t)
		called := false
		r.OnError = func(_ string, _ error) {
			called = true
			for _, s := range r.Snapshot() {
				if s.Name == "/Q" && s.State != "empty" || s.Name != "/Q" && s.State != "full" {
					t.Errorf("bad failure marking: %+v", s)
				}
			}
		}
		if err := r.AddTransition(Transition{Name: "/fail", Inputs: []string{"/A"}, Outputs: []string{"/Q"}, Run: fn}); err != nil {
			t.Fatal(err)
		}
		if err := r.Fire(context.Background(), "/fail"); err == nil {
			t.Fatal("expected failure")
		}
		if !called {
			t.Fatal("missing error notification")
		}
	}
}

func TestTimeoutReleasesBeforeUnderlyingWorkReturns(t *testing.T) {
	r := basic(t)
	release := make(chan struct{})
	exited := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	fn := func(context.Context, map[string]Token) (map[string]Token, error) {
		defer close(exited)
		<-release
		tok, _ := NewToken("Bool", false)
		return map[string]Token{"/Q": tok}, nil
	}
	if err := r.AddTransition(Transition{Name: "/timeout", Inputs: []string{"/A"}, Outputs: []string{"/Q"}, Timeout: time.Millisecond, Run: fn}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- r.Fire(context.Background(), "/timeout") }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waited for underlying work")
	}
	// A fresh attempt can own the same places; the old result has no commit authority.
	tok, _ := r.Token("/A")
	if err := r.AddTransition(Transition{Name: "/fresh", Inputs: []string{"/A"}, Outputs: []string{"/Q"}, Run: func(context.Context, map[string]Token) (map[string]Token, error) {
		return map[string]Token{"/Q": tok}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Fire(context.Background(), "/fresh"); err != nil {
		t.Fatal(err)
	}
	// Wait for the old implementation before checking that its result cannot replace Q.
	close(release)
	<-exited
	q, ok := r.Token("/Q")
	if !ok || q.Hash != tok.Hash {
		t.Fatal("late completion replaced fresh result")
	}
}

func TestReservationConflictAndAtomicAcquisition(t *testing.T) {
	r := basic(t)
	started := make(chan struct{})
	release := make(chan struct{})
	tok, _ := NewToken("Bool", true)
	if err := r.AddTransition(Transition{Name: "/slow", Inputs: []string{"/A"}, Outputs: []string{"/Q"}, Run: func(context.Context, map[string]Token) (map[string]Token, error) {
		close(started)
		<-release
		return map[string]Token{"/Q": tok}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- r.Fire(context.Background(), "/slow") }()
	<-started
	if err := r.AddTransition(Transition{Name: "/conflict", Inputs: []string{"/B", "/A"}, Outputs: []string{"/B"}, Run: func(context.Context, map[string]Token) (map[string]Token, error) { return nil, nil }}); err != nil {
		t.Fatal(err)
	}
	if err := r.Fire(context.Background(), "/conflict"); !errors.Is(err, ErrNotEnabled) {
		t.Fatal(err)
	}
	for _, s := range r.Snapshot() {
		if s.Name == "/B" && s.State != "full" {
			t.Fatal("partial reservation")
		}
		if s.Name == "/A" && s.State != "full+input-reserved" {
			t.Fatal("execution changed marking")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTokenCanonicalObjects(t *testing.T) {
	a, err := NewToken("Record", struct {
		Z int
		A int
	}{2, 1})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewToken("Record", map[string]int{"A": 1, "Z": 2})
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash != b.Hash || string(a.Data) != string(b.Data) {
		t.Fatal("object order changed identity")
	}
	c, err := NewToken("Other", map[string]int{"A": 1, "Z": 2})
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash == c.Hash {
		t.Fatal("type tags must separate token identities")
	}
}

func TestFullOutputRejectsWithoutChangingMarking(t *testing.T) {
	r := basic(t)
	tok, _ := NewToken("Bool", true)
	if err := r.Put("/Q", tok); err != nil {
		t.Fatal(err)
	}
	ran := false
	if err := r.AddTransition(Transition{Name: "/blocked", Inputs: []string{"/A"}, Outputs: []string{"/Q"}, Run: func(context.Context, map[string]Token) (map[string]Token, error) { ran = true; return nil, nil }}); err != nil {
		t.Fatal(err)
	}
	if err := r.Fire(context.Background(), "/blocked"); !errors.Is(err, ErrNotEnabled) {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("ran with full output")
	}
	for _, s := range r.Snapshot() {
		if s.State != "full" {
			t.Fatalf("rejection changed marking or locks: %+v", s)
		}
	}
}

func TestConsumerUsesActualUnreservedTokenDespiteOutputLock(t *testing.T) {
	r := basic(t)
	// Model the lock/token combination directly; lock metadata must not act as an input arc.
	r.places["/A"].outputOwner = 99
	if err := r.AddTransition(Transition{Name: "/consume", Inputs: []string{"/A"}, Outputs: []string{"/Q"}, Run: func(_ context.Context, in map[string]Token) (map[string]Token, error) {
		return map[string]Token{"/Q": in["/A"]}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Fire(context.Background(), "/consume"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Token("/A"); ok {
		t.Fatal("did not consume actual input")
	}
	if r.places["/A"].outputOwner != 99 {
		t.Fatal("consumer released another attempt's output lock")
	}
	if _, ok := r.Token("/Q"); !ok {
		t.Fatal("missing output")
	}
}
