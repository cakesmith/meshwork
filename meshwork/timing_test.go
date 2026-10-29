package meshwork

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func advance(t *testing.T, c *ManualClock, r *Runtime, d time.Duration) {
	t.Helper()
	if err := c.Advance(d); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(testContext(t)); err != nil {
		t.Fatal(err)
	}
}
func timedRuntime(t *testing.T, timeout, delay time.Duration) (*Runtime, *ManualClock, *Attempt) {
	t.Helper()
	c := NewManualClock(time.Unix(0, 0))
	r := NewWithClock(c)
	for _, p := range []Place{{"/in", "Bool"}, {"/out", "Bool"}} {
		if err := r.AddPlace(p); err != nil {
			t.Fatal(err)
		}
	}
	token, _ := NewToken("Bool", true)
	if err := r.Put("/in", token); err != nil {
		t.Fatal(err)
	}
	if err := r.AddTransition(Transition{Name: "/work", Inputs: []string{"/in"}, Outputs: []string{"/out"}, Timeout: timeout, Run: func(ctx context.Context, _ map[string]Token) (map[string]Token, error) {
		if err := WaitClock(ctx, "execution", delay, false); err != nil {
			return nil, err
		}
		return map[string]Token{"/out": token}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	a, err := r.Start(testContext(t), "/work")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(testContext(t)); err != nil {
		t.Fatal(err)
	}
	return r, c, a
}
func TestManualTimeoutBoundariesAndRelease(t *testing.T) {
	for i := 0; i < 40; i++ {
		r, c, a := timedRuntime(t, 30*time.Second, time.Hour)
		advance(t, c, r, 29*time.Second)
		select {
		case <-a.Done():
			t.Fatal("early expiration")
		default:
		}
		before := r.Snapshot()
		if before[0].State != "full+input-reserved" || before[1].State != "empty+output-locked" {
			t.Fatal(before)
		}
		advance(t, c, r, time.Second)
		if err := a.Wait(testContext(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		after := r.Snapshot()
		if after[0].State != "full" || after[1].State != "empty" {
			t.Fatal(after)
		}
		advance(t, c, r, time.Second)
		if !reflect.DeepEqual(after, r.Snapshot()) {
			t.Fatal("expiration duplicated or marking changed")
		}
	}
}
func TestCompletionAndDeadlineArbitration(t *testing.T) {
	for _, tc := range []struct {
		name           string
		timeout, delay time.Duration
		expired        bool
	}{
		{"completion first", 30 * time.Second, 29 * time.Second, false},
		{"deadline equality", 30 * time.Second, 30 * time.Second, true},
		{"timeout first", 30 * time.Second, 31 * time.Second, true},
		{"omitted is infinite", 0, 100000 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < 20; i++ {
				r, c, a := timedRuntime(t, tc.timeout, tc.delay)
				d := tc.delay
				if tc.expired {
					d = tc.timeout
				}
				advance(t, c, r, d)
				err := a.Wait(testContext(t))
				if tc.expired {
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				_, present := r.Token("/out")
				if present == tc.expired {
					t.Fatal("wrong commit result")
				}
				advance(t, c, r, time.Hour)
				_, present = r.Token("/out")
				if present == tc.expired {
					t.Fatal("late commit")
				}
			}
		})
	}
}
func TestManualTimeoutRejectsUncooperativeLateResult(t *testing.T) {
	ctx := testContext(t)
	c := NewManualClock(time.Unix(0, 0))
	r := NewWithClock(c)
	for _, p := range []Place{{"/A", "Bool"}, {"/Q", "Bool"}} {
		if err := r.AddPlace(p); err != nil {
			t.Fatal(err)
		}
	}
	token, _ := NewToken("Bool", true)
	_ = r.Put("/A", token)
	release, exited := make(chan struct{}), make(chan struct{})
	defer close(release)
	if err := r.AddTransition(Transition{Name: "/slow", Inputs: []string{"/A"}, Outputs: []string{"/Q"}, Timeout: 30 * time.Second, Run: func(context.Context, map[string]Token) (map[string]Token, error) {
		<-release
		close(exited)
		other, _ := NewToken("Bool", false)
		return map[string]Token{"/Q": other}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	errorsSeen := 0
	r.OnError = func(_ string, err error) {
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Error(err)
		}
		errorsSeen++
	}
	a, err := r.Start(ctx, "/slow")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Advance(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := a.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := r.AddTransition(Transition{Name: "/fresh", Inputs: []string{"/A"}, Outputs: []string{"/Q"}, Run: func(context.Context, map[string]Token) (map[string]Token, error) {
		return map[string]Token{"/Q": token}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Fire(ctx, "/fresh"); err != nil {
		t.Fatal(err)
	}
	// Execute the old completion path directly after finalization as well.
	r.finish(a, map[string]Token{"/Q": token}, nil)
	if errorsSeen != 1 {
		t.Fatalf("error notifications: %d", errorsSeen)
	}
	q, ok := r.Token("/Q")
	if !ok || q.Hash != token.Hash {
		t.Fatal("old attempt replaced fresh output")
	}
	_ = exited
}
func TestIndependentTimeoutsShareClock(t *testing.T) {
	ctx := testContext(t)
	c := NewManualClock(time.Unix(0, 0))
	r := NewWithClock(c)
	attempts := []*Attempt{}
	for i, d := range []time.Duration{10 * time.Second, 30 * time.Second, 0} {
		in, out, name := fmt.Sprintf("/in%d", i), fmt.Sprintf("/out%d", i), fmt.Sprintf("/work%d", i)
		_ = r.AddPlace(Place{in, "Bool"})
		_ = r.AddPlace(Place{out, "Bool"})
		token, _ := NewToken("Bool", true)
		_ = r.Put(in, token)
		if err := r.AddTransition(Transition{Name: name, Inputs: []string{in}, Outputs: []string{out}, Timeout: d, Run: func(ctx context.Context, _ map[string]Token) (map[string]Token, error) {
			return nil, WaitClock(ctx, "execution", 0, true)
		}}); err != nil {
			t.Fatal(err)
		}
		a, err := r.Start(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		attempts = append(attempts, a)
	}
	defer func() {
		for _, a := range attempts {
			_ = r.ExpireAttempt(a.ID())
		}
	}()
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	advance(t, c, r, 10*time.Second)
	if err := attempts[0].Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case <-attempts[1].Done():
		t.Fatal("independent deadline expired early")
	default:
	}
	advance(t, c, r, 20*time.Second)
	if err := attempts[1].Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	advance(t, c, r, 100000*time.Hour)
	select {
	case <-attempts[2].Done():
		t.Fatal("infinite attempt expired")
	default:
	}
}
func TestManualClockTimers(t *testing.T) {
	c := NewManualClock(time.Unix(0, 0))
	a, b := c.NewTimer(time.Second), c.NewTimer(time.Second)
	if !b.Stop() || b.Stop() {
		t.Fatal("timer stop")
	}
	if err := c.Advance(-time.Second); err == nil {
		t.Fatal("clock moved backwards")
	}
	if err := c.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-a.C():
	default:
		t.Fatal("due timer not delivered synchronously")
	}
	_ = c.Advance(time.Second)
	select {
	case <-a.C():
		t.Fatal("timer delivered twice")
	default:
	}
	select {
	case <-b.C():
		t.Fatal("stopped timer fired")
	default:
	}
}

func TestTimeoutControlSupervisesAttempt(t *testing.T) {
	for _, delay := range []time.Duration{29 * time.Second, 30 * time.Second, 31 * time.Second} {
		for repeat := 0; repeat < 20; repeat++ {
			ctx := testContext(t)
			clock := NewManualClock(time.Unix(0, 0))
			g, err := (Library{}).Flatten(TimeoutControlGraph(true), "/control")
			if err != nil {
				t.Fatal(err)
			}
			g.Places = append(g.Places, Place{"/work", "Bool"}, Place{"/result", "Bool"})
			g.Transitions = append(g.Transitions, AtomicTransition{Name: "/worker", Implementation: "work", Inputs: []string{"/work"}, InputPorts: []string{"work"}, Outputs: []string{"/result"}, OutputPorts: []string{"result"}})
			implementations := TimingImplementations()
			implementations["work"] = func(ctx context.Context, in map[string]Token) (map[string]Token, error) {
				if err := WaitClock(ctx, "execution", delay, false); err != nil {
					return nil, err
				}
				return map[string]Token{"result": in["work"]}, nil
			}
			r, err := g.InstantiateWithClock(implementations, clock)
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]any{"/work": true, "/control/duration": "30s", "/control/clock": "execution"} {
				typ := "Bool"
				if name == "/control/duration" {
					typ = "Duration"
				}
				if name == "/control/clock" {
					typ = "Clock"
				}
				token, _ := NewToken(typ, value)
				if err := r.Put(name, token); err != nil {
					t.Fatal(err)
				}
			}
			worker, err := r.Start(ctx, "/worker")
			if err != nil {
				t.Fatal(err)
			}
			token, _ := NewToken("Attempt", worker.ID())
			if err := r.Put("/control/attempt", token); err != nil {
				t.Fatal(err)
			}
			controller, err := r.Start(ctx, "/control/timeout")
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if delay < 30*time.Second {
				advance(t, clock, r, delay)
				advance(t, clock, r, 30*time.Second-delay)
			} else {
				advance(t, clock, r, 30*time.Second)
			}
			err = worker.Wait(ctx)
			if delay < 30*time.Second {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("supervised deadline did not win", err)
			}
			if err := controller.Wait(ctx); err != nil {
				t.Fatal(err)
			}
			if _, ok := r.Token("/control/timeout_event"); !ok {
				t.Fatal("no deadline event")
			}
			_, present := r.Token("/result")
			if present != (delay < 30*time.Second) {
				t.Fatal("wrong supervised marking")
			}
		}
	}
}
