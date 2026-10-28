# Active Context

## State
COMPOSITION_MILESTONE_COMPLETE (2026-10-28). Next: specify place-based subgraph selection before implementing it.

## Grounding protocol
Before each request, read this file and relevant technical/code files. Update systemPatterns.md or techContext.md before architectural changes. Record discrepancies immediately, and update this file at milestone/session closure. Treat the current user handoff as authoritative; earlier Portfolio/browser/multiset designs describe a different checkout and do not override this strictly 1-safe Go model.

## Baseline inspection
- All five fundamental Memory Bank documents already existed and were read/updated.
- Found a Go kernel and NAND demo, with five tests. No prototype archive was present inside the workspace.
- Baseline tests/demo passed using installed Go 1.27.3; the old Go 1.22.3/toolchain-download blocker was stale.
- Working tree already contained many Kotlin/Gradle deletions, modified README/Memory Bank files, and untracked Go sources. Preserved that migration state; did not restore, stage, or commit files.

## Completed milestone
- `meshwork/graph.go`: serializable canonical graph definitions and executable graphs; nominal typed interfaces; deterministic namespace resolver; composition; reusable entities/architectures; explicit port bindings; finite flattening; sorted output; definition source trace paths; separate implementation registry; validated runtime instantiation with copied slices.
- `meshwork/logic.go`: reusable NAND and duplication architectures, composed NOT, and composed OR. OR expands to nine 1-safe places and five transitions (two duplicators and three NANDs).
- `meshwork/core.go`: strengthened name validation, normalized JSON object keys, checked token content/hash at Put and output boundaries, isolated token inspection, panic cleanup, and prompt timeout/cancellation finalization with no late commit. Protected place-map type reads with the runtime mutex.
- `meshwork/graph_test.go`: truth tables for NAND/NOT/OR; serialized graph round-trip execution; equal-name sharing versus independent names; deterministic declaration ordering; collision/type/binding/reference/recursion errors; bipartite endpoints; runtime isolation from later graph edits.
- `meshwork/core_test.go`: preserved existing tests and added self-loop, immutability, tampered token, panic/invalid output restoration and callback, prompt timeout and late completion, competing reservation/atomic acquisition, token identity normalization, full-output rejection, and consumer/output-lock tests.
- `cmd/demo/main.go`: all four OR cases through architecture expansion and the existing firing kernel. No hardcoded OR expression. Its acyclic firing loop is example-specific, not a general scheduler.
- README explains APIs, semantics, examples, limits, and next milestone. All five Memory Bank documents updated. `.cache/.gitignore` excludes generated verification cache files without restoring the pre-existing deleted root .gitignore.

## Verification
Final checks passed after all application/test edits:
- `go test ./...`
- `go vet ./...`
- `go test -race ./...`
- `go run ./cmd/demo`: false/false -> false; false/true, true/false, true/true -> true; five transitions each.

Used `$env:GOCACHE = Join-Path (Get-Location) '.cache/go-build'` after a default-cache access-denied error. No dependencies or module-version changes. One initial new test had a double-close cleanup bug; fixed and reran successfully. Another negative test caught implicit missing-place creation during expansion; explicit referring-graph place declarations are now required.

## Open boundaries / limits
- Place-based graph selection needs an agreed traversal/branching/cycle policy and transition closure. Slice and subgraph remain the same concept.
- Immutable definition revision storage and cross-language numeric JSON canonicalization are not implemented. Numeric spelling is currently identity-significant. Nominal types do not validate JSON value schemas.
- Default runtime error policy is undecided; only optional OnError exists. Configure it before concurrent execution.
- Finite timeouts revoke logical authority but cannot forcibly stop goroutines or external effects. Implementations/adapters own stopping/reconciliation. No retries/backoff policy in the kernel.
- Composite architecture deadlines are rejected rather than silently changed into per-transition timeouts. Initial outputs must be empty except self-loops. Entities currently require nonempty inputs/outputs and distinct port names.
- No subgraph extraction, general scheduler, persistence, cloud adapter, GraphML/Mermaid tools, UI, or Collator implementation was added.

## Next smallest useful milestone
Specify place-based subgraph selection with dependency-complete transition closure, explicit typed boundary places, and tests for branches/cycles. Resolve only policies needed for that milestone. Preserve the graph/marking/firing kernel and computational identities.
