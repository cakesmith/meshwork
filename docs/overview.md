# Meshwork overview

Meshwork is a toolkit for typed, strictly bipartite, 1-safe Petri nets, currently implemented in Go. It provides a canonical graph model, graph composition, runtime firing, file linking, scenarios, and a small CLI. It does not ship application transition implementations.

## The model

`meshwork.Graph` is the canonical representation. It contains typed places, atomic transition definitions, and optional references to other graphs. `ExecutableGraph` is an alias for the same representation, not a second model.

Transitions connect to places through directed arcs; place-to-place and transition-to-transition arcs are invalid. A place holds zero or one immutable typed token. References select a graph explicitly and can bind or relocate names. Linking resolves references while retaining them; flattening expands finite references into one graph. Compatible equal resolved names unify; conflicts and recursive references fail.

The [distributed example](../examples/distributed/) shows the intended ownership boundary. Graph YAML and Go implementations live in the consuming project, which creates the `map[string]Compute` used to instantiate a graph. An `implementation` value is an opaque consumer registry key: Meshwork neither imports consumer packages nor dynamically loads Go source.

## Execution

When a transition starts, Meshwork atomically reserves its input tokens and locks its output places. The computation runs outside the marking lock. On success, inputs are consumed and outputs are produced atomically. On failure, panic, cancellation, invalid output, or timeout, reservations and locks are released and the input marking remains unchanged. An optional `OnError` callback runs after release.

Graph timeouts are either finite Go-duration strings or infinite. Runtime clocks are injected: `RealClock` uses elapsed time and `ManualClock` advances only when instructed. A result that reaches its deadline exactly is treated as timed out. Cancellation cannot undo external effects performed by a Go implementation.

## Files, scenarios, and adapters

The linker discovers strict YAML graph and scenario files, resolves repositories and manifests, then validates or executes the selected graph. GraphML is an opt-in import format and remains presentation/provenance separate from graph semantics. Semantic comparison reports graph changes independently from source provenance and conservative rename suggestions.

A finite scenario supplies a marking and expected output. The CLI can execute the included distributed consumer examples because it knows that example's registry; other consumers can supply their own registry from Go or bind independently running transition executors through an explicit [deployment](remote-execution.md).

A singular scenario using `input: stdin` and `output: stdout` is a persistent NDJSON adapter. Graph source places accept stdin frames, graph sink places emit stdout frames, and the parent process owns shutdown and resource limits. It is intentionally not a network service or general scheduler.

## Boundaries

Meshwork currently has no automatic retries, distributed scheduler, cloud adapter, UI, dynamic Go loading, immutable revisions, cross-language numeric normalization, or general cyclic/reactive scheduling. Scenarios fire transitions at most once. See [distributed authoring](linker.md), [timing](timing.md), and the [usage walkthrough](../README.md).
