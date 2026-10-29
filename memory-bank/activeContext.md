# Active Context

## State
ARCHITECTURAL_SYNCHRONIZATION_VERIFIED (2026-10-29). Authoritative handoff implemented and final checks passed. Existing memory-bank was read before application edits; systemPatterns.md/techContext.md were updated before architectural changes. Earlier Portfolio/multiset designs do not override this typed, strictly bipartite, 1-safe Go model.

## Changes in this milestone
- meshwork/graph.go and logic.go: removed Entity/Architecture and both registries; Graph now contains places, transitions and direct references. ExecutableGraph aliases Graph. Library.Graphs, Link/Flatten/Compose provide explicit graph selection, namespace/name bindings, deterministic normalization and preserved sharing. Linked stage retains references; flattened stage expands them.
- linker/files.go, link.go, graphml.go, diff.go: removed obsolete contracts/semantic diff fields; added graph-defined duration parsing, conflict comparison, GraphML timeout property and YAML finite-timeout export. Diff compares same-stage references/bindings as well as places, transitions and directed arcs; provenance stays separate.
- meshwork/core.go, new attempt.go/clock.go/timing.go: injected real/manual clocks with stopped unused timers, unique attempt IDs, asynchronous Start and existing Fire API, atomic commit/timeout finalization, late-result rejection and lock/reservation release. Deadline equality wins provisionally. Ordinary timeout/delay graphs use clock capabilities and optional supervised attempts; register supervised commit bounds before waiting.
- linker/scenario.go: existing singular/plural schema gains clock selection and manual steps, strict validation against computation substitution, cooperative due-work draining before assertions and cancellation/release on explicit stopping steps. Each transition still fires at most once.
- New examples/timing graph/scenarios and timing regressions in meshwork/linker. Existing truth tables, distributed fixtures, CLI, import/diff and invariants preserved. README.md/docs/linker.md updated; docs/timing.md added; all five fundamental Memory Bank documents synchronized.

## Validation
Final checks passed on Go 1.27.3 with the existing workspace-local caches:
- go test ./... -count=1
- go test -race ./... -count=1
- go vet ./...
- go run ./cmd/demo: all four OR results correct, five transitions each.
- Distributed CLI validation: 42 places, 23 transitions, 17 scenarios.
- Timing CLI validation: 3 places, 1 transition, 2 scenarios.
- GraphML/YAML NAND CLI diff: no semantic changes, seven independent provenance changes.
- git diff --check: clean after normalizing edited Markdown to repository LF line endings.

No Go/dependency changes, staging or commits. All 26 requested invariant/behavior categories are covered by the retained and extended regressions. Finite/infinite, boundary, arbitration and supervised-control cases run repeatedly; tests verify behavior for these finite examples rather than proving arbitrary graph execution. Test coverage includes manual 29/30/31-second boundaries, finite/infinite timeouts, completion/deadline equality, late results, independent deadlines/shared clocks, supervised attempts, repeated deterministic outcomes, explicit identity sharing/independence, bipartiteness, scenario substitution rejection, linked reference diff and mixed-stage rejection. Test watchdogs use wall-time only to fail hangs; success does not depend on elapsed wall time.

## Open boundaries and next focus
- Default runtime error-handler policy and final tie-breaking convention remain undecided. Existing optional OnError callback remains; errors are returned with no automatic recovery.
- Scenarios are finite, one firing per transition. Manual tests require cooperative WaitClock for quiescence; arbitrary blocking implementations require cancellation and can outlive an expired attempt. External effects cannot be rolled back.
- A controller deadline remains a commit bound once armed, including if its controller is canceled. Runtime failure assertions in YAML are not yet expressible; next smallest useful milestone is explicit expected-failure scenario assertions.
- File linking/CLI comparison eagerly flatten; Go Library.Link and Diff support unflattened canonical graphs. YAML export still rejects remapped local ports. Types remain nominal without schema validation; immutable revisions/numeric normalization/place-selection traversal remain future work.
- Actual yEd save/reopen remains unverified. No GraphML XML rewriting/synchronization, distributed scheduling, cloud adapters, retries, promises, UI or Collator code. No dependencies, Go version changes, staging or commits. Preserve pre-existing .cache.

## Grounding protocol
Before each new request, read this file and relevant code/technical context. Update systemPatterns.md or techContext.md before architectural edits. Record discrepancies immediately and update this state at milestones/closure; provide the human a memory-update checklist.

## Latest inspection (2026-10-29)
- Moved linker compatibility details (obsolete entity/architecture rejection, mixed-source completion and effective-timeout conflicts) from docs/linker.md into the memory bank. The public GraphML key table now labels `meshwork.implementation` only as the transition implementation; registry/path mechanics remain internal context.
- Removed relative implementation-source normalization. `implementation`/`meshwork.implementation` is now opaque end to end: `./Nand.kt#compute` remains that literal key and is resolved only from the caller's `map[string]Compute` at instantiation. No linker reads, validates or derives a source path.
- Validation after this edit: `go test ./linker ./meshwork -count=1` and `git diff --check` passed using workspace-local Go caches.

## Documentation boundary adjustment (2026-10-29)
- Removed "registry key" implementation mechanics from the public GraphML property table. `meshwork.implementation` remains required runtime identity, not a relative source path: `Instantiate` uses it to choose the consumer-supplied `map[string]Compute` function. Its opaque-key rule and source-file convention remain in system/technical memory context only.
- No parser, schema, graph, or runtime behavior changed. Next work must not remove the property without first selecting a replacement executable binding contract.

## Pending architecture decision (2026-10-29)
- User clarified that a transition's Go implementation must live beside its graph YAML. Current implementation identifiers are intentionally opaque keys resolved only by the caller's `map[string]Compute`; Go cannot dynamically compile or load an adjacent source file. Choose the intended binding mechanism before code changes, then update systemPatterns.md and techContext.md.

## Consumer-owned implementation examples (2026-10-29)
- Established the core rule: Meshwork owns graph/runtime contracts only; consuming projects keep Go transition implementations beside graph YAML and explicitly compose the registry used for execution. Meshwork does not import, replace, discover, compile, or dynamically load consumer implementation sources.
- Added distributed-example implementations next to their graphs: `primitives/implementation/logic.nand.go` and `primitives/fanout/logic.duplicate.go`. `examples/distributed/implementations.go` is the consumer-owned binding boundary for their existing YAML keys.
- Redirected distributed NAND/OR/MUX scenario regressions to that consumer registry and documented the boundary in README.md and docs/linker.md. Kept `meshwork/LogicLibrary` unchanged as the core's self-contained Boolean demo; it is no longer used to execute the distributed consumer example.
- Updated projectBrief.md, productContext.md, systemPatterns.md, and techContext.md before code to reflect the principle and Go packaging model.
- Validation passed with workspace-local caches: `go test ./... -count=1`, `go vet ./...`, and `git diff --check`.

## Remove bundled Boolean implementation (2026-10-29)
- Removed `meshwork/logic.go`, including the shipped NAND/duplication registry and Boolean graph/demo API. Meshwork now ships no application transition implementation.
- `cmd/demo` now links the distributed YAML consumer and executes it with `examples/distributed/implementations.go`. The demo still reports all four OR truth-table results with five transitions each.
- GraphML/import tests now obtain their NAND function from the distributed consumer registry. Core composition tests use private `meshwork/graph_fixture_test.go` fixtures only; those fixtures are not part of the built Meshwork package.
- Updated README.md plus systemPatterns.md and techContext.md before removal. Validation passed: `go test ./... -count=1`, `go vet ./...`, `go test -race ./...`, `go run ./cmd/demo`, and `git diff --check`.
