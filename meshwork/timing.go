package meshwork

import (
	"context"
	"fmt"
	"time"
)

// TimeoutControlGraph reports a deadline event. The optional attempt input
// additionally revokes that execution's authority to commit.
func TimeoutControlGraph(supervise bool) Graph {
	g := Graph{
		Places:      []Place{{"duration", "Duration"}, {"clock", "Clock"}, {"timeout_event", "TimeoutEvent"}},
		Transitions: []AtomicTransition{{Name: "timeout", Implementation: "meshwork.timeout", Inputs: []string{"duration", "clock"}, InputPorts: []string{"duration", "clock"}, Outputs: []string{"timeout_event"}, OutputPorts: []string{"timeout_event"}}},
	}
	if supervise {
		g.Places = append(g.Places, Place{"attempt", "Attempt"})
		g.Transitions[0].Inputs = append(g.Transitions[0].Inputs, "attempt")
		g.Transitions[0].InputPorts = append(g.Transitions[0].InputPorts, "attempt")
	}
	return g
}

// DelayGraph waits before reporting an event, without supervising another attempt.
func DelayGraph() Graph {
	g := TimeoutControlGraph(false)
	g.Places[2] = Place{"event", "Event"}
	g.Transitions[0].Name = "delay"
	g.Transitions[0].Implementation = "meshwork.delay"
	g.Transitions[0].Outputs = []string{"event"}
	g.Transitions[0].OutputPorts = []string{"event"}
	return g
}

// TimingImplementations use runtime-provided clocks, never wall-clock sleeps.
func TimingImplementations() map[string]Compute {
	timed := func(timeout bool) Compute {
		return func(ctx context.Context, in map[string]Token) (map[string]Token, error) {
			var duration, capability string
			if err := in["duration"].Decode(&duration); err != nil {
				return nil, err
			}
			if err := in["clock"].Decode(&capability); err != nil {
				return nil, err
			}
			infinite := duration == "infinite"
			var d time.Duration
			var err error
			if !infinite {
				d, err = time.ParseDuration(duration)
				if err != nil || d < 0 {
					return nil, fmt.Errorf("invalid duration %q", duration)
				}
			}
			var supervised uint64
			if token, ok := in["attempt"]; ok {
				if !timeout {
					return nil, fmt.Errorf("delay cannot supervise attempts")
				}
				if err := token.Decode(&supervised); err != nil {
					return nil, err
				}
				if supervised == 0 {
					return nil, fmt.Errorf("invalid attempt identity")
				}
			}
			if err = waitClock(ctx, capability, d, infinite, supervised); err != nil {
				return nil, err
			}
			typ := "Event"
			if timeout {
				typ = "TimeoutEvent"
			}
			event, err := NewToken(typ, true)
			port := "event"
			if timeout {
				port = "timeout_event"
			}
			return map[string]Token{port: event}, err
		}
	}
	return map[string]Compute{"meshwork.timeout": timed(true), "meshwork.delay": timed(false)}
}
