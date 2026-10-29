# Technical Context

## Verified stack
- Module example.com/meshwork-prototype; Go baseline 1.23 in go.mod.
- Installed Go 1.27.3 windows/amd64, verified 2026-10-29.
- Go modules with direct pinned gopkg.in/yaml.v3 v3.0.1; kernel/composition use standard library. No new dependency or runtime version changes.
- Windows PowerShell; gcc is available for race detection.
- Use workspace-local GOCACHE=.cache/go-build and GOMODCACHE=.cache/go-mod when sandbox default-cache permissions fail. Both are populated. Preserve the pre-existing untracked .cache directory.

## Formats and execution
Only graph.<name>.meshwork.yaml and <purpose>.scenario[s].<name>.meshwork.yaml authoring files. GraphML import is opt-in. Strict YAML rejects unknown fields and scenarios cannot redefine computation. Entity/architecture fields/properties no longer exist semantically. `implementation`/`meshwork.implementation` is an opaque caller-supplied `map[string]Compute` key, conventionally named for an adjacent consumer source file, never a relative source-file reference.

Consumer implementation files are ordinary Go packages placed beside their graph YAML. They import `meshwork` to implement `Compute`; a consumer-owned package composes their functions into the `map[string]Compute` passed to `Instantiate` or `RunScenario`. Meshwork does not dynamically load `.go` files, so the YAML implementation value remains a registry key rather than a Go import path.

`meshwork` contains no shipped Boolean implementation registry. The distributed consumer example supplies `logic.nand.go` and `logic.duplicate.go`; the demo links its YAML and uses that example's registry. Core tests use private `_test.go` fixtures.

Graph transition timeout uses a nonnegative Go duration string or infinite; omission/0s means infinite. Canonical JSON finite durations are integer nanoseconds and zero is omitted. Canonical YAML exports finite timeouts, but rejects local port remappings it cannot retain. Conflicting declarations must agree on effective timeout, including the default.

Scenario clock type is real (default) or manual, with nonnegative finite start offset from Unix epoch. Manual-only steps contain one finite nonnegative advance_clock or expect. Step states empty/present check actual marking; other values are typed token expectations. A collection may supply a default clock. Existing top-level expect keeps value semantics. Runtime failure still fails a scenario; explicit expected-failure syntax remains future work. CLI validates only; RunScenario executes.

Clock interface uses Now and NewTimer with stoppable timers; both clocks also expose After. Manual scenario work uses WaitClock/Settle and no sleeps. Test wall-time contexts are watchdogs only. Runtime contexts cancel on finalized expiry without pretending a manual clock is a wall-clock context deadline.

GraphML: UTF-8 standard GraphML namespace, 8 MiB/file, 100000 elements, depth 128. DOCTYPE/directives/external entities, hyperedges and computational ports remain unsupported. Presentation/raw XML remains preserved separately. Actual yEd save/reopen is unverified.

## Verification commands
```powershell
$env:GOCACHE = Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE = Join-Path (Get-Location) '.cache/go-mod'
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/demo
go run ./cmd/meshwork validate ./examples/distributed/primitives ./examples/distributed/composites
go run ./cmd/meshwork validate ./examples/timing
go run ./cmd/meshwork diff ./linker/testdata/graphml/graph.nand.graphml ./linker/testdata/graphml/graph.nand.meshwork.yaml
git diff --check
```

Baseline full tests passed before changes. Full tests also pass after the new clock/scenario/supervision regressions; final validation is recorded in activeContext.md. Docs: README.md, docs/linker.md, docs/timing.md. Examples: existing distributed NAND/OR/MUX and new examples/timing timeout/infinite scenarios.
