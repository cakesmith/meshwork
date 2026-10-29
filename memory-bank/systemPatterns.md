# System Patterns

## Authoritative model (2026-10-29)
Graphs compose graphs. Scenarios execute graphs. Adapters realize graphs. This supersedes earlier entity/architecture contracts, registries and metadata, which have been removed. No automatic instance namespace or third definition type.

`meshwork.Graph` is the canonical IR: Places, Transitions and optional References, with a resolution Stage. `ExecutableGraph` is an alias, not a second model. `Library.Graphs` holds graph definitions; implementations are separately supplied `map[string]Compute`. `Library.Link` resolves local identities/bindings and retains references at Stage linked. `Flatten` expands finite references and emits sorted places/transitions with no references and an omitted stage marker (flattened). `Compose` joins declarations then flattens. Default reference namespaces preserve target identities; only explicit namespace relocation/bindings separate objects. Bindings can remap places or transitions. Conflicts, recursion, duplicate arcs and node-kind collisions fail. Local port mappings remain attached to endpoints.

Implementation ownership is inverted from framework ownership: a consuming project places Go implementation files beside its graph YAML and explicitly composes a `map[string]Compute` at its application boundary. Meshwork accepts that map only when instantiating; it must not import consumer packages or inspect/compile source files. The YAML implementation string is the consumer's stable registry key, conventionally matching the adjacent source filename.

The core ships no application Boolean graphs or NAND/duplication implementations. Core composition tests define private fixtures; executable demonstrations link a consumer project and use its registry.

The distributed YAML/GraphML linker continues eager flattening into the same IR. Collect all contributions before resolving; compatible resolved names merge, conflicting semantic declarations halt linking with an empty Result. File references choose graphs explicitly; scenarios only bind marking/expectation names. Result.Definitions contains selected regions closed over selected transitions' endpoint places, not reachability-derived selections. Sources and original GraphML/presentation remain sidecars.

## Firing, attempts and clocks
Runtime.Start atomically reserves input tokens and output places before returning an Attempt with immutable ID(), Done and Wait. Fire wraps Start/Wait. Computation executes outside the marking mutex; finish serializes exactly one validated commit or failure, releases reservations/locks and calls optional OnError after unlocking. Actual marking remains intact during execution. A failed/expired attempt cannot affect newer ownership or commit a late result.

Graph-defined atomic timeout zero means infinite. Clock exposes Now/NewTimer with stoppable Timer.C/Stop; RealClock uses Go time, ManualClock synchronously delivers due timers on Advance. Attempt expiry is checked again under the runtime mutex before commit. Equality expires the attempt, a provisional documented convention. Deadline finalization does not physically terminate goroutines/external effects. No retry/error-policy framework.

TimeoutControlGraph and DelayGraph are ordinary typed places/transitions using TimingImplementations and immutable Duration/Clock tokens. The execution capability identifies the injected runtime clock; RegisterClock supports further named capabilities. Optional Attempt tokens supervise an active same-runtime attempt. Register its commit bound before parking so deadline equality does not depend on callback scheduling. An armed supervised deadline remains a commit bound if its controller is canceled.

WaitClock registers cooperative waits under the runtime mutex. Settle blocks until active work completes or parks on a future timer; due waits must run before it returns. Manual scenario steps start eligible work in name order, settle, advance/check and repeat. Each transition starts at most once. Final steps stop execution and cancel/wait for pending attempts. Arbitrary blocking adapters need explicit cancellation/cooperation; no general scheduler is implied.

## Comparison and import
Diff/DiffResults compare matching linked/flattened stages, types, transition implementations/timeouts, directed arcs/local ports and linked graph references/bindings. Reference target/namespace changes remove/add references; binding edits report reference_binding_changed. Provenance is independent; visual XML/group/ID changes do not affect computation. CLI comparisons load flattened graphs. Preserve deterministic ordering, input purity, structural validation and conservative rename suggestions.

GraphML uses bounded standard-library XML parsing; semantic keys include meshwork.timeout and meshwork.implementation. An implementation is an opaque registry key resolved only by `Instantiate` against the caller-supplied `map[string]Compute`; linkers neither resolve relative source paths nor inspect source files. Removed entity/architecture keys are rejected when used. Incomplete types/implementations/topology retain diagnostics; structural conflicts always fail. Canonical JSON strips provenance and omits infinite timeout; YAML preserves finite timeouts but rejects remapped ports it cannot express. No XML writing.

Linker compatibility is implementation detail, not user-facing format guidance: YAML-only linking requires a complete executable declaration; compatible GraphML/YAML contributions may fill a missing type or implementation. Timeout omission is the infinite default, so it conflicts with any finite declaration of the same transition.

## Main files
- meshwork/core.go: immutable tokens, places/marking, atomic acquisition, Start/Fire.
- meshwork/attempt.go: attempt finalization, deadline arbitration, capabilities, WaitClock/Settle.
- meshwork/clock.go and timing.go: real/manual clocks, ordinary timeout/delay graphs and implementations.
- meshwork/graph.go: canonical graph composition; tests provide private graph fixtures only.
- linker/files.go, link.go, graphml.go: discovery, strict parsing, resolution, merge, canonicalization and source sidecars.
- linker/scenario.go and diff.go: isolated finite execution/manual steps and semantic comparison.
- cmd/meshwork: discover/link/validate/import/diff; cmd/demo: consumer-owned distributed OR truth table.
