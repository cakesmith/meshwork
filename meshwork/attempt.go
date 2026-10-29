package meshwork

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type attemptKey struct{}

// Attempt identifies one reservation and its single atomic final decision.
type attemptDeadline struct {
	clock Clock
	at    time.Time
}

func (d attemptDeadline) due() bool { return !d.clock.Now().Before(d.at) }

type Attempt struct {
	supervised []attemptDeadline
	id         uint64
	runtime    *Runtime
	transition Transition
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	timer      Timer
	deadline   time.Time
	finalized  bool
	err        error
	waiting    bool
	waitClock  Clock
	wakeAt     time.Time
}

func (a *Attempt) ID() uint64            { return a.id }
func (a *Attempt) Done() <-chan struct{} { return a.done }
func (a *Attempt) Wait(ctx context.Context) error {
	select {
	case <-a.done:
		return a.err
	case <-ctx.Done():
		a.runtime.finish(a, nil, ctx.Err())
		<-a.done
		return a.err
	}
}
func (a *Attempt) execute(inputs map[string]Token) {
	var result map[string]Token
	var err error
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("transition %s panicked: %v", a.transition.Name, p)
		}
		a.runtime.finish(a, result, err)
	}()
	result, err = a.transition.Run(a.ctx, inputs)
}
func (a *Attempt) expired() bool {
	if !a.deadline.IsZero() && !a.runtime.clock.Now().Before(a.deadline) {
		return true
	}
	for _, d := range a.supervised {
		if d.due() {
			return true
		}
	}
	return false
}
func (r *Runtime) notifyLocked() { close(r.changed); r.changed = make(chan struct{}) }

func (r *Runtime) finish(a *Attempt, result map[string]Token, err error) {
	r.mu.Lock()
	if a.finalized {
		r.mu.Unlock()
		return
	}
	// Deadline equality expires the attempt, even if completion is also ready.
	if a.expired() {
		err = context.DeadlineExceeded
	}
	if err == nil {
		err = a.ctx.Err()
	}
	t := a.transition
	if err == nil {
		if len(result) != len(t.Outputs) {
			err = fmt.Errorf("transition %s returned %d outputs, expected %d", t.Name, len(result), len(t.Outputs))
		} else {
			for _, n := range t.Outputs {
				tok, ok := result[n]
				if !ok {
					err = fmt.Errorf("missing output %s", n)
					break
				}
				if tok.Type != r.places[n].definition.Type {
					err = fmt.Errorf("wrong output type for %s", n)
					break
				}
				if e := validateToken(tok); e != nil {
					err = fmt.Errorf("output %s: %w", n, e)
					break
				}
			}
		}
	}
	if err == nil {
		for _, n := range t.Inputs {
			r.places[n].token = nil
		}
		for _, n := range t.Outputs {
			cp := result[n].Clone()
			r.places[n].token = &cp
		}
	}
	for _, n := range t.Inputs {
		r.places[n].inputOwner = 0
	}
	for _, n := range t.Outputs {
		r.places[n].outputOwner = 0
	}
	a.finalized = true
	a.err = err
	if a.timer != nil {
		a.timer.Stop()
	}
	a.cancel()
	handler := r.OnError
	r.mu.Unlock()
	if err != nil && handler != nil {
		handler(t.Name, err)
	}
	r.mu.Lock()
	delete(r.active, a.id)
	close(a.done)
	r.notifyLocked()
	r.mu.Unlock()
}

// Settle waits for active work to finish or explicitly park on a future clock
// timer. Advancing a manual clock alone never implies its consumers have run.
func (r *Runtime) Settle(ctx context.Context) error {
	for {
		r.mu.Lock()
		var expired []*Attempt
		settled := true
		for _, a := range r.active {
			if !a.finalized && a.expired() {
				expired = append(expired, a)
			}
			if a.finalized || !a.waiting || (!a.wakeAt.IsZero() && !a.waitClock.Now().Before(a.wakeAt)) {
				settled = false
			}
		}
		changed := r.changed
		r.mu.Unlock()
		if len(expired) > 0 {
			sort.Slice(expired, func(i, j int) bool { return expired[i].id < expired[j].id })
			for _, a := range expired {
				r.finish(a, nil, context.DeadlineExceeded)
			}
			continue
		}
		if settled {
			return ctx.Err()
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// RegisterClock supplies a named capability used by ordinary clock tokens.
// Configure capabilities before starting computations.
func (r *Runtime) RegisterClock(name string, clock Clock) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "" || clock == nil || r.clocks[name] != nil {
		return fmt.Errorf("invalid or duplicate clock capability %q", name)
	}
	r.clocks[name] = clock
	return nil
}
func clockCapability(ctx context.Context, name string) (Clock, *Attempt, error) {
	a, ok := ctx.Value(attemptKey{}).(*Attempt)
	if !ok {
		return nil, nil, fmt.Errorf("clock capability requires a runtime attempt")
	}
	r := a.runtime
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.clocks[name]
	if c == nil {
		return nil, nil, fmt.Errorf("unknown clock capability %q", name)
	}
	return c, a, nil
}

// WaitClock cooperatively waits on an injected capability. Infinite waits have
// no timer; callers use zero duration for an immediate delay.
func WaitClock(ctx context.Context, capability string, d time.Duration, infinite bool) error {
	return waitClock(ctx, capability, d, infinite, 0)
}
func waitClock(ctx context.Context, capability string, d time.Duration, infinite bool, supervise uint64) error {
	if d < 0 {
		return fmt.Errorf("negative delay")
	}
	c, a, err := clockCapability(ctx, capability)
	if err != nil {
		return err
	}
	r := a.runtime
	r.mu.Lock()
	if err := ctx.Err(); err != nil {
		r.mu.Unlock()
		return err
	}
	if supervise != 0 {
		target := r.active[supervise]
		if target == nil || target.finalized {
			r.mu.Unlock()
			return fmt.Errorf("unknown or finalized attempt %d", supervise)
		}
		if !infinite {
			target.supervised = append(target.supervised, attemptDeadline{clock: c, at: c.Now().Add(d)})
		}
	}
	var timer Timer
	var due <-chan time.Time
	if !infinite {
		a.wakeAt = c.Now().Add(d)
		timer = c.NewTimer(d)
		due = timer.C()
	}
	a.waiting = true
	a.waitClock = c
	r.notifyLocked()
	r.mu.Unlock()
	if timer != nil {
		defer timer.Stop()
	}
	select {
	case <-due:
		err = ctx.Err()
	case <-ctx.Done():
		err = ctx.Err()
	}
	r.mu.Lock()
	if supervise != 0 && err == nil {
		target := r.active[supervise]
		r.mu.Unlock()
		if target != nil {
			r.finish(target, nil, context.DeadlineExceeded)
		}
		r.mu.Lock()
	}
	a.waiting = false
	a.wakeAt = time.Time{}
	r.notifyLocked()
	r.mu.Unlock()
	return err
}

// ExpireAttempt revokes one attempt's commit authority; it does not kill work.
func (r *Runtime) ExpireAttempt(id uint64) error {
	r.mu.Lock()
	a := r.active[id]
	r.mu.Unlock()
	if a == nil {
		return fmt.Errorf("unknown or finalized attempt %d", id)
	}
	r.finish(a, nil, context.DeadlineExceeded)
	return nil
}
