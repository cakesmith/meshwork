# Technical Context

## Semantic diff additions (2026-10-28)
- No dependency or Go version changes. linker.Diff compares ExecutableGraph; DiffResults adds Result.Sources. Report JSON uses typed values and stable semanticChanges/provenanceChanges/possibleRenames arrays; text uses Report.WriteText.
- CLI: go run ./cmd/meshwork diff BEFORE AFTER --format text|json [--yed]. Files may be GraphML/YAML; directories discover both formats under stable repository name workspace. Named multi-repository comparisons use existing discovery/linking APIs, then DiffResults. Successful differences exit zero; invalid inputs fail without a report.
- Final checks use existing .cache/go-build and .cache/go-mod after default-cache access denied: go test ./..., go vet ./..., go test -race ./..., CLI text/JSON smoke checks and git diff --check. Scale regression uses 3000 places/2999 transitions; no performance benchmark claim.

## GraphML additions (2026-10-28)
- No new dependencies or runtime changes: Go encoding/xml, encoding/json, existing yaml.v3.
- XML limits: 8 MiB/file, 100000 elements, nesting depth 128; UTF-8 with optional BOM. DOCTYPE/directives and unknown entities fail; no external entity resolver. Require standard GraphML namespaces. Computational edges must be directed; hyperedges and computational GraphML ports unsupported.
- Commands: `go run ./cmd/meshwork import ./linker/testdata/graphml/graph.nand.graphml`, `validate` with the same path, `import FILE --format yaml`, and `link ROOT --graphml-root DIAGRAM_ROOT`. Repeat discovery roots as needed; `--yed` explicitly enables shape/label inference. JSON import output includes diagnostics and raw XML/provenance; default import output is a summary.
- API: ImportGraphML(filename, conventions), DiscoverGraphML(repositories, conventions), LinkFiles(combinedFiles), CanonicalJSON(linked.Graph), CanonicalYAML(linked.Graph). YAML-only Discover/Link remain opt-out of GraphML.
- Test/vet/race, original demo, GraphML CLI smoke checks and existing 17 distributed scenarios pass with workspace-local GOCACHE/GOMODCACHE. Default-cache compilation attempted a blocked module-cache network request; using the already populated local caches resolved it without downloads or dependency edits.
- Real yEd save/reopen remains unverified; fixtures are hand-authored in yEd format. File contract resolution and perfect round-trip synchronization remain deferred. YAML export rejects timeout/remapped-port information it cannot retain.

## Verified stack (2026-10-28)
- Module: `example.com/meshwork-prototype`.
- Language baseline: Go 1.23 (`go.mod`).
- Installed toolchain: `go1.27.3 windows/amd64`.
- Kernel/composition use the standard library. The linker imports `gopkg.in/yaml.v3 v3.0.1` directly, pinned in go.mod/go.sum; Go modules manage dependencies.
- Windows/PowerShell; gcc is available for the Go race detector.
- Graph definitions and executable graphs serialize through encoding/json; timeout durations serialize as integer nanoseconds.

## Commands
```powershell
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/demo
go run ./cmd/meshwork discover ./examples/distributed/primitives ./examples/distributed/composites
go run ./cmd/meshwork link ./examples/distributed/primitives ./examples/distributed/composites
go run ./cmd/meshwork validate ./examples/distributed/primitives ./examples/distributed/composites
```

The initial run succeeded with the default cache, but a subsequent command hit access denied under `C:\Users\nicks\AppData\Local\go-build`. Verification uses a workspace-local cache via `$env:GOCACHE = Join-Path (Get-Location) '.cache/go-build'`. This is an execution-environment setting, not a project dependency. `.cache/.gitignore` ignores generated cache contents. For ordinary use, a writable cache under `$env:TEMP` also works.

The historical Go 1.22.3/toolchain-download warning is resolved. No Go language-version change was needed.

Distributed-linker verification also sets `$env:GOMODCACHE = Join-Path (Get-Location) '.cache/go-mod'`. The sandbox initially denied network access for `go get`; an approved elevated download populated the workspace cache. All subsequent test/vet/race/demo/CLI commands ran with workspace-local caches. No compiler or Go directive changes were made. `docs/linker.md` specifies file schemas, repository configuration, identity rules, and execution limits.

Prerequisite verification on 2026-10-28 used the same installed Go 1.27.3 and workspace-local caches. `go test ./...`, `go vet ./...`, `go test -race ./...`, the original OR demo, and distributed CLI validation passed. Validation now reports 42 places, 23 transitions, 17 scenarios after adding reusable NOT/MUX YAML fixtures. No dependencies, runtime versions, application code, or configuration changed in this verification task.
