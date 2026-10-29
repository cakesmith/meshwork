# Clocks, timeouts and scenarios

Timeouts belong to atomic transition definitions. Omitted or zero timeout means infinite; YAML also accepts explicit `infinite`. A finite timeout revokes an execution attempt's authority to commit, releases reservations/locks, preserves input marking, produces no normal output and invokes the configured `OnError` callback. It does not forcibly terminate an implementation or undo external effects. The default callback policy remains undecided.

## Injected clocks and arbitration

```go
type Clock interface {
    Now() time.Time
    NewTimer(time.Duration) Timer
}
type Timer interface {
    C() <-chan time.Time
    Stop() bool
}
```

This uses stoppable timers instead of only `After` so completed attempts release unused deadlines. Real and manual clocks also expose `After` for convenience. `RealClock` uses Go's monotonic time where available. `NewManualClock(start)` advances only through `Advance(nonnegativeDuration)`; due buffered timer deliveries happen before Advance returns, once per timer. Consumers still need to run afterward.

`NewWithClock` and `Graph.InstantiateWithClock` inject the execution clock. `Runtime.Start` reserves places synchronously and returns an `Attempt` with an ID, `Done` and `Wait`. `Fire` remains the synchronous convenience method. Finalization is serialized under the runtime mutex, with exactly one marking decision and error notification. At commit arbitration, a due deadline beats completion, including equality. This is the documented provisional convention, not a finalized product-level tie policy. Successful completion finalized before the deadline stays committed. Late computations cannot release a newer attempt's locks or replace its result.

Set `OnError` before starting work. Runtime contexts are canceled when an attempt finalizes; a manual deadline is not represented as a wall-clock context deadline. Code needing an injected delay must use `WaitClock(ctx, capability, duration, infinite)` rather than `time.Sleep` or wall-clock context timers.

## Ordinary timeout-control graphs

`TimeoutControlGraph(false)` has `duration: Duration`, `clock: Clock` and `timeout_event: TimeoutEvent` places, connected through one ordinary `meshwork.timeout` transition. `DelayGraph()` uses the same clock machinery and returns an `Event`, without supervising another attempt. Both can be referenced and bound like any other graph.

Duration tokens carry a Go duration string such as `30s` or `infinite`. Clock tokens carry an immutable capability name. Every runtime supplies `execution`, referring to its injected clock. `RegisterClock` adds further named clock capabilities before execution. Neither clocks nor execution attempts are new vertex kinds.

`TimeoutControlGraph(true)` adds an `attempt: Attempt` input carrying an active attempt ID from the same runtime. Before parking, the implementation registers a supervised commit deadline under the runtime mutex. This prevents a result from committing at/after that deadline even if the timer consumer has not yet run. On expiration it finalizes the supervised attempt and emits a deadline event. Completion finalized before expiration remains committed; the controller still reports the elapsed deadline. Unknown/already-finalized attempt IDs fail at registration. The deadline remains a commit bound once armed, including if the controller is subsequently canceled; no automatic retry or cancellation policy is implied.

Use `TimingImplementations()` to bind `meshwork.timeout` and `meshwork.delay`. These are ordinary implementation functions supplied explicitly by the caller, not an automatically loaded registry. `examples/timing` contains the equivalent YAML timeout graph and two manual-clock scenarios.

## Manual scenario steps

```yaml
graph: /timing/timeout
scenarios:
  - name: expires_at_30_seconds
    clock: {type: manual, start: 0s}
    marking: {duration: 30s, clock: execution}
    steps:
      - advance_clock: 29s
      - expect: {timeout_event: empty}
      - advance_clock: 1s
      - expect: {timeout_event: present}
      - advance_clock: 1s
      - expect: {timeout_event: true}
```

`start` is a nonnegative offset from the Unix epoch; it does not request wall-clock waiting. Each step has exactly one operation. Clock advancement accepts only finite nonnegative durations. Step `empty`/`present` checks the actual marking, not lock metadata. Other expectation values become the existing immutable nominally typed tokens. Top-level `expect` retains its existing value semantics.

```go
result, err := linker.Link([]linker.Repository{{Name: "timing", Root: "./examples/timing"}})
if err != nil { return err }
for _, scenario := range result.Scenarios {
    _, err := linker.RunScenario(ctx, result.Definitions[scenario.Graph], scenario,
        meshwork.TimingImplementations())
    if err != nil { return err }
}
```

The finite runner starts eligible transitions in name order, waits for running work to finish or explicitly park on a future clock wait, and repeats after every advance before checking expectations. `Runtime.Settle` provides that coordination. A due timer prevents its waiting computation from being considered settled. There are no scheduling sleeps or assumptions about immediate timer callback execution.

Each transition fires at most once. The last explicit step is a stopping condition; remaining attempts are canceled and finalized before return. Arbitrary external work that neither completes nor parks through `WaitClock` cannot declare manual-time quiescence; callers must provide cancellation, and general adapter/scheduler coordination remains separate work. Runtime failures, including transition deadlines, still fail the scenario; expected-failure assertions are the next small extension. No GraphML XML writing, general scheduler, retry framework or promise resolution is included.
