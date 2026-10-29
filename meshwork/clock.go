package meshwork

import (
	"fmt"
	"sync"
	"time"
)

// Clock uses stoppable timers so successful attempts do not retain unused deadlines.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
}
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}
type RealClock struct{}

func (RealClock) Now() time.Time                         { return time.Now() }
func (RealClock) NewTimer(d time.Duration) Timer         { return realTimer{time.NewTimer(d)} }
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type realTimer struct{ *time.Timer }

func (t realTimer) C() <-chan time.Time { return t.Timer.C }

// ManualClock advances only through Advance; due timers are delivered synchronously.
type ManualClock struct {
	mu     sync.Mutex
	now    time.Time
	timers map[*manualTimer]time.Time
}
type manualTimer struct {
	clock *ManualClock
	ch    chan time.Time
}

func NewManualClock(start time.Time) *ManualClock {
	return &ManualClock{now: start, timers: map[*manualTimer]time.Time{}}
}
func (c *ManualClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *ManualClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &manualTimer{clock: c, ch: make(chan time.Time, 1)}
	if d <= 0 {
		t.ch <- c.now
	} else {
		c.timers[t] = c.now.Add(d)
	}
	return t
}
func (c *ManualClock) After(d time.Duration) <-chan time.Time { return c.NewTimer(d).C() }
func (t *manualTimer) C() <-chan time.Time                    { return t.ch }
func (t *manualTimer) Stop() bool {
	c := t.clock
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.timers[t]
	delete(c.timers, t)
	return ok
}
func (c *ManualClock) Advance(d time.Duration) error {
	if d < 0 {
		return fmt.Errorf("manual clock cannot move backwards")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	for t, deadline := range c.timers {
		if !c.now.Before(deadline) {
			t.ch <- deadline
			delete(c.timers, t)
		}
	}
	return nil
}
