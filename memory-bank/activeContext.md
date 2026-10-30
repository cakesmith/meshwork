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

## CLI/linker boundary review (2026-10-29)
- Confirmed the executable boundary: `cmd/meshwork` is a thin human-facing adapter; `linker` is the reusable file-format/discovery/link/diagnostic API consumed by the CLI, demo, tests, and prospective consumers. They must remain separate packages, but their user-facing verbs can shrink.
- Current CLI exposes internal pipeline stages as `discover`, `link`, `validate`, and `import`, plus the distinct `diff`. `link` and `validate` both discover and link; `import` is a GraphML-specific presentation of the same linker result; `discover` is a debugging/API concern. A future simplification should retain an inspection/check command, an explicit export command if canonical serialization is required, and `diff`; make GraphML an automatically recognized input instead of a separate mode. Preserve the Go linker API and explicit multi-repository configuration for advanced composition.
- No application code, command behavior, or tests changed during this review. Next step is a product decision on whether canonical export needs a CLI command; only then change the command surface and its tests/docs.

## Demo relocation (2026-10-29)
- Moved the consumer-owned OR truth-table executable from `cmd/demo/main.go` to `examples/distributed/demo/main.go`. `cmd/` now contains only the Meshwork CLI entry point; the demonstration stays with the consumer graph and implementation registry it exercises.
- Updated README.md, systemPatterns.md, and techContext.md to use `go run ./examples/distributed/demo`. Verified with workspace-local Go caches: `go test ./... -count=1`, `go run ./examples/distributed/demo`, and `git diff --check`.

## CLI scenario inquiry (2026-10-29)
- Confirmed the CLI boundary for `examples/distributed/composites/tests/tests.scenarios.mux.meshwork.yaml`: `meshwork validate` discovers, links, and validates its declarations and references, but never executes transition implementations. Invoke it with both distributed repository roots: `go run ./cmd/meshwork validate ./examples/distributed/primitives ./examples/distributed/composites`.
- Executable scenario coverage belongs to Go tests because the consumer supplies the implementation registry. `go test ./linker -run '^TestDistributedExamples$'` runs the linked distributed scenarios (including the MUX fixture); no CLI option selects or executes one scenario file.

## CLI scenario execution (2026-10-29)
- Added `meshwork execute --scenario FILE`: it discovers/links configured repositories, selects scenarios by exact source path, invokes `linker.RunScenario` through the included distributed consumer registry, and returns one `passed: NAME` line per successful scenario. It returns a nonzero error for an undiscovered file, runtime/assertion failure, or unknown implementation key; it does not dynamically load arbitrary consumer Go files.
- Moved distributed execution coverage from `linker.TestDistributedExamples` into `cmd/meshwork.TestExecuteScenarioFile`. The linker test now verifies linking/shape/scenario discovery only; the CLI regression runs the eight MUX cases from `tests.scenarios.mux.meshwork.yaml`.
- Documented the exact invocation in README.md and docs/linker.md. Validation passed: `go test ./cmd/meshwork ./linker -count=1`, `go vet ./...`, `go run ./cmd/meshwork execute --scenario ./examples/distributed/composites/tests/tests.scenarios.mux.meshwork.yaml -repo primitives=./examples/distributed/primitives -repo composites=./examples/distributed/composites`, `go test ./... -count=1`, and `git diff --check`.

## Repository dependency manifests (2026-10-29)
- Added optional `meshwork.repositories.yaml` support to `linker.Discover`. A manifest declares `repositories: {name: relative/path}`; discovery resolves dependencies recursively from the declaring root, retains declared repository names, inherits the configured parent namespace, and rejects duplicate names/roots, empty paths, malformed YAML, and unknown fields. It does not discover or load Go implementation sources.
- Added `examples/distributed/composites/meshwork.repositories.yaml` declaring `primitives: ../primitives`. `meshwork validate ./examples/distributed/composites` now links composites and primitives without a second CLI root; the MUX command is `meshwork execute --scenario ./examples/distributed/composites/tests/tests.scenarios.mux.meshwork.yaml ./examples/distributed/composites`.
- Added linker regression coverage for manifest dependency resolution and strict unknown-field rejection. Updated README.md/docs/linker.md. Validation passed with workspace-local caches: `go test ./... -count=1`, `go vet ./...`, the composites-only validate/execute commands, and `git diff --check`.

## Scenario-root inference (2026-10-29)
- `meshwork execute --scenario FILE` now derives its sole repository root when callers provide neither a positional root nor `--repo`: it walks upward from FILE to the nearest directory containing `meshwork.repositories.yaml`. This uses the declared composites boundary and preserves explicit-root behavior for unmanifested repositories.
- The MUX invocation is now `go run ./cmd/meshwork execute --scenario ./examples/distributed/composites/tests/tests.scenarios.mux.meshwork.yaml`, with no redundant composites argument. Updated the CLI regression and README/linker docs.
- Validation passed with workspace-local caches: shortened MUX execution (all eight cases), `go test ./... -count=1`, `go vet ./...`, and `git diff --check`.

## Standard-input/output scenario adapter (2026-10-29)
- Added the smallest standalone execution adapter to singular scenarios: `input: stdin` and `output: PLACE`. It keeps file concerns outside the graph model: callers use shell redirection, stdin is one bounded YAML initial-marking mapping, and stdout is the selected final token's canonical JSON payload.
- Linking rejects malformed adapter declarations, including a non-stdin mode, a missing output, plural scenarios, or fixed `marking`/`expect`. `linker.WithStdinMarking` validates the received document before runtime instantiation: one mapping/document, at most 1 MiB, known resolved places and declared nominal token types. Execution/runtime/output absence still returns an error, so the CLI process exits nonzero and prints no success value.
- Converted `examples/distributed/composites/run.scenario.nand.meshwork.yaml` to the adapter (`input: stdin`, `output: Q`), added CLI regressions for a valid NAND input and malformed/unknown/multiple-document inputs, and documented build/piping usage in README.md and docs/linker.md.
- Validation passed with workspace-local caches: `go test ./... -count=1`, `go vet ./...`, `'{A: true, B: true}' | go run ./cmd/meshwork execute --scenario ./examples/distributed/composites/run.scenario.nand.meshwork.yaml` (prints `false`), and `git diff --check`.

## Stdio-to-port feasibility review (2026-10-29)
- No application code changed. The existing adapter is deliberately one-shot: it reads one bounded (1 MiB) YAML mapping from stdin, executes, and writes one final canonical JSON payload to stdout.
- Shell redirection alone cannot bind TCP/UDP ports. A separate supervised bridge process may listen on a loopback TCP connection, pass the received request bytes to one `meshwork execute` child stdin, and return that child stdout on the same connection. Separate ingress and egress listeners require request/response correlation and lifecycle rules, so they are not an equivalent simple reroute.
- Do not expose such a bridge beyond loopback without authentication, resource/concurrency limits, framing/EOF semantics, and an explicit protocol; it would execute user-controlled scenario inputs.

## Persistent stdin/stdout execution adapter (2026-10-29)
- Replaced the one-shot YAML stdin adapter with a singular streaming scenario contract: `input: stdin`, `output: stdout`, no fixed marking/expect/steps/manual clock. Graph source places are ingress and graph sink places are egress; both directions use NDJSON `{ "place": "NAME", "value": JSON }` frames. EOF deliberately does not stop the process; its parent must terminate it.
- Finite `expect` scenarios now return as soon as every expected place has a token, comparing the complete expected marking then. They no longer require unrelated transitions to become enabled or finish.
- Added runtime `Changed` and atomic `Take` support. Service inputs respect occupied-place backpressure; service output is written before the sink token is removed. Internal places cannot be injected or drained.
- Migrated the distributed NAND adapter scenario and README/linker documentation. Added CLI stream framing/error regression, finite early-expect regression, and runtime take regression.
- Validation passed using workspace-local Go caches: `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`, and `git diff --check`.

## Scenario execution plugins (2026-10-29)
- Added `linker/execution_plugins.go`: the CLI now delegates every selected scenario to `linker.ExecuteScenario`; it no longer switches on stdin/stdout behavior in `cmd/meshwork/main.go`.
- Exactly one enabled `ExecutorPlugin` owns execution. `TestExecutionPlugin` enables for top-level or step expectations and retains finite pass output; `StreamExecutionPlugin` enables otherwise and owns persistent NDJSON execution. Enabled `ExecutionObserver` plugins run before/after the selected executor, providing the extension point for simultaneous metrics and other cross-cutting plugins.
- Added host regression coverage for observer ordering, executor exclusivity, and built-in selection. Existing finite and stream CLI regressions remain green.
- Validation passed using workspace-local Go caches: `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`, and `git diff --check`.

## Plugin package boundary (2026-10-29)
- Moved the execution-plugin host, test executor, stream executor, and their tests from `linker/` into the root `plugins/` package. The dependency direction is now `cmd/meshwork -> plugins -> linker -> meshwork`; linker does not import plugins.
- `linker.Scenario.ResolveInput` is the only new linker-facing helper for the stream plugin, preserving resolved scenario bindings without exposing linker parser internals. `linker.RunScenario` remains the finite execution primitive used by the test plugin.
- `cmd/meshwork` composes `plugins.BuiltIns()` and calls `plugins.ExecuteScenario`; stream handling is no longer in linker or the command switch. Plugin host regression coverage moved with the package.
- Validation passed using workspace-local Go caches: `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`, and `git diff --check`.
