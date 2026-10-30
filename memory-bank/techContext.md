# Technical Context

## Implementation milestone (2026-10-30)
Implement explicit place/port YAML and arc transport bindings, plus a standard-library HTTP/JSON executor transport. Deployment configuration supplies authority listen/advertise endpoints, named executors and transition overrides. Keep code/dependency/runtime versions unchanged. Preserve existing native scenarios. Remote manual-clock/capability operations are unsupported and must fail explicitly; network execution is a single-authority, non-durable foundation, not distributed consensus. Validate bounded protocol messages, cancellation, invalid/late outputs and unchanged marking on failure.

## Verified stack
- Module example.com/meshwork-prototype; Go baseline 1.23 in go.mod.
- Installed Go 1.27.3 windows/amd64, verified 2026-10-29.
- Go modules with direct pinned gopkg.in/yaml.v3 v3.0.1; kernel/composition use standard library. No new dependency or runtime version changes.
- Windows PowerShell; gcc is available for race detection.
- Use workspace-local GOCACHE=.cache/go-build and GOMODCACHE=.cache/go-mod when sandbox default-cache permissions fail. Both are populated. Preserve the pre-existing untracked .cache directory.

## Formats and execution
Only graph.<name>.meshwork.yaml and <purpose>.scenario[s].<name>.meshwork.yaml authoring files. GraphML import is opt-in. Strict YAML rejects unknown fields and scenarios cannot redefine computation. Entity/architecture fields/properties no longer exist semantically. `implementation`/`meshwork.implementation` is an opaque caller-supplied `map[string]Compute` key, conventionally named for an adjacent consumer source file, never a relative source-file reference.

`meshwork.repositories.yaml` is an optional repository-root manifest with `repositories: {name: relative/path}`. Discovery loads listed roots recursively before linking; dependency paths resolve from their declaring root, retain the specified repository name, and inherit the caller's parent namespace. It supplies graph dependencies only and does not load implementation source files.

Consumer implementation files are ordinary Go packages placed beside their graph YAML. They import `meshwork` to implement `Compute`; a consumer-owned package composes their functions into the `map[string]Compute` passed to `Instantiate` or `RunScenario`. Meshwork does not dynamically load `.go` files, so the YAML implementation value remains a registry key rather than a Go import path.

The `meshwork execute --scenario FILE` CLI command is wired only to the included `examples/distributed` consumer registry. It links its configured repositories and passes each selected scenario to the root `plugins` package, rather than branching on scenario input in `main.go`. The built-in test executor prints `passed: NAME`; the stream executor owns NDJSON I/O. The host permits simultaneously enabled observer plugins (for example metrics) while requiring exactly one executor. It returns an error for an undiscovered file, unknown implementation key, or failed scenario; it is not a plugin mechanism for arbitrary consumers.

A singular scenario with an `expect` block is finite: execution ends when all expected places contain tokens and compares that expected marking. A singular no-expect adapter uses `input: stdin` and `output: stdout`; it is a persistent NDJSON service. Each stdin line is `{ "place": "NAME", "value": JSON }` targeting a graph source place, and each stdout line represents a consumed graph sink token. EOF does not stop the service; its parent terminates or cancels it. Input/output files use ordinary shell redirection; Meshwork YAML contains no host paths. This mode is limited to the CLI and the bundled distributed registry.

When `execute --scenario FILE` has no positional root or `--repo`, the CLI walks upward from FILE and uses the nearest directory containing `meshwork.repositories.yaml` as its root. This is limited to a declared boundary; projects without that manifest still provide an explicit root or repository flags.

`meshwork` contains no shipped Boolean implementation registry. The distributed consumer example supplies `logic.nand.go` and `logic.duplicate.go`; finite scenario execution uses that example's registry through the CLI. Core tests use private `_test.go` fixtures.

Graph transition timeout uses a nonnegative Go duration string or infinite; omission/0s means infinite. Canonical JSON finite durations are integer nanoseconds and zero is omitted. Canonical YAML exports finite timeouts, but rejects local port remappings it cannot retain. Conflicting declarations must agree on effective timeout, including the default.

Scenario clock type is real (default) or manual, with nonnegative finite start offset from Unix epoch. Manual-only steps contain one finite nonnegative advance_clock or expect. Step states empty/present check actual marking; other values are typed token expectations. A collection may supply a default clock. Existing top-level expect keeps value semantics. Runtime failure still fails a scenario; explicit expected-failure syntax remains future work. CLI validate checks declarations; CLI execute delegates through scenario plugins.

Clock interface uses Now and NewTimer with stoppable timers; both clocks also expose After. Manual scenario work uses WaitClock/Settle and no sleeps. Test wall-time contexts are watchdogs only. Runtime contexts cancel on finalized expiry without pretending a manual clock is a wall-clock context deadline.

GraphML: UTF-8 standard GraphML namespace, 8 MiB/file, 100000 elements, depth 128. DOCTYPE/directives/external entities, hyperedges and computational ports remain unsupported. Presentation/raw XML remains preserved separately. Actual yEd save/reopen is unverified.

## Verification commands
```powershell
$env:GOCACHE = Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE = Join-Path (Get-Location) '.cache/go-mod'
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/meshwork validate ./examples/distributed/primitives ./examples/distributed/composites
go run ./cmd/meshwork validate ./examples/timing
go run ./cmd/meshwork diff ./linker/testdata/graphml/graph.nand.graphml ./linker/testdata/graphml/graph.nand.meshwork.yaml
git diff --check
```

Baseline full tests passed before changes. Full tests also pass after the new clock/scenario/supervision regressions; final validation is recorded in activeContext.md. Docs: README.md, docs/linker.md, docs/timing.md. Examples: existing distributed NAND/OR/MUX and new examples/timing timeout/infinite scenarios.

## Plugin package composition (2026-10-30)
- Share scenario plugins as ordinary Go packages explicitly imported by a host; no dynamic loader or source discovery. The CLI composes built-in executor packages separately from the distributed example registry. Scenario schema and graph repository linking do not load plugin code.

## Proposed portable contracts (2026-10-30)
- `docs/plugin-architecture.md` is a transport-neutral target only: linker, runtime, effects, executors and observers are replaceable compiled plugins. Current Go APIs and explicit registry remain implemented and unchanged. Portable token encoding/hash and numeric rules, protocol versions, capabilities, error/cancellation semantics, plugin binding/distribution, transport and bootstrap are not yet specified or implemented.

## Proposed per-transition execution (2026-10-30)
- Target executors are selected per transition from compiled binaries, with a default and overrides; arc-associated transports connect transition output and input ports through a place. No executable format, wire protocol, source schema, locality optimization, distributed runtime or negotiation is implemented. See `docs/plugin-architecture.md` for unsettled ownership and failure semantics.

## Proposed place authority (2026-10-30)
- `docs/place.md` describes one logical authority per graph instance, including place state and atomic multi-place firing. No distributed transport, durable recovery or place protocol exists yet; byte placement and authority failure behavior remain undecided.

## Current remote transport and configuration (2026-10-30)
- `meshwork execute --scenario FILE --deployment FILE ROOT` binds a selected graph using an explicit deployment YAML. Native executors retain the caller's registry; HTTP/JSON executors export their own effects. The compiled example is `go build -o executor.exe ./examples/distributed/executor`; `examples/remote/executor.py` is an independent standard-library peer.
- Protocol `meshwork.executor.v1`: authenticated GET /capabilities and POST /execute; ephemeral input capability GET URLs; typed JSON values with authority-computed hashes. 4 MiB messages, correlation checks, no redirects/retries. Default listener is loopback/ephemeral; cross-machine use requires explicit reachable authority advertise URL. HTTP listeners require a trusted private network or external TLS termination.
- Go packages/dependencies unchanged. Remote effects require real clocks; native timing/manual clock behavior remains. Example and exact commands are in `docs/remote-execution.md`.
