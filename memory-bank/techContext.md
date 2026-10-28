# Technical Context

## Verified stack (2026-10-28)
- Module: `example.com/meshwork-prototype`.
- Language baseline: Go 1.23 (`go.mod`).
- Installed toolchain: `go1.27.3 windows/amd64`.
- Standard library only; no go.sum, external packages, or package manager required.
- Windows/PowerShell; gcc is available for the Go race detector.
- Graph definitions and executable graphs serialize through encoding/json; timeout durations serialize as integer nanoseconds.

## Commands
```powershell
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/demo
```

The initial run succeeded with the default cache, but a subsequent command hit access denied under `C:\Users\nicks\AppData\Local\go-build`. Verification uses a workspace-local cache via `$env:GOCACHE = Join-Path (Get-Location) '.cache/go-build'`. This is an execution-environment setting, not a project dependency. `.cache/.gitignore` ignores generated cache contents. For ordinary use, a writable cache under `$env:TEMP` also works.

The historical Go 1.22.3/toolchain-download warning is resolved. No Go language-version change was needed.
