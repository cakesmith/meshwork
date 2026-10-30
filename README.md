# Meshwork

A Go prototype for typed, strictly bipartite, 1-safe Petri nets. **Graphs compose graphs. Scenarios execute graphs. Adapters realize graphs.** The kernel uses the standard library; YAML authoring uses pinned `gopkg.in/yaml.v3`.

## Run

Go 1.23 or later:

```powershell
go test ./...
go vet ./...
go test -race ./...
go run ./examples/distributed/demo
go run ./cmd/meshwork validate ./examples/distributed/composites

go run ./cmd/meshwork execute --scenario ./examples/distributed/composites/tests/tests.scenarios.mux.meshwork.yaml
go run ./cmd/meshwork validate ./examples/timing
```

Build the CLI and run its persistent stdin/stdout NAND adapter with ordinary shell redirection:

```powershell
go build -o meshwork.exe ./cmd/meshwork
'{"place":"A","value":true}' | .\meshwork.exe execute --scenario ./examples/distributed/composites/run.scenario.nand.meshwork.yaml
# keeps running; send B on the same stream to receive {"place":"/logic/nand/Q","value":false}
```

That singular scenario declares `input: stdin` and `output: stdout`. It is a persistent NDJSON adapter: each stdin line is `{ "place": "NAME", "value": JSON }`, accepted only for graph source places; each graph sink token is emitted as NDJSON and removed after emission. It runs until its parent terminates it; EOF does not end the session.

The demo prints all four correct OR results, using two duplication and three NAND transitions. Distributed validation reports 42 places, 23 transitions and 17 NAND/OR/MUX scenarios; timing validation reports 3 places, 1 transition and 2 scenarios. CLI validation never executes implementation code. Tests execute the scenarios through the Go API. The race detector requires a supported C compiler.

If the default caches are inaccessible in the sandbox, use the populated workspace caches:

```powershell
$env:GOCACHE = Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE = Join-Path (Get-Location) '.cache/go-mod'
```

## Representation and composition

`meshwork.Graph` contains typed places, atomic transition definitions and optional graph references with explicit namespace/name bindings. `ExecutableGraph` is an alias for this same representation. `Library.Graphs` holds ordinary graph definitions; implementation functions live separately in `map[string]Compute`.

```go
linked, err := linker.Link([]linker.Repository{
    {Name: "primitives", Root: "examples/distributed/primitives"},
    {Name: "composites", Root: "examples/distributed/composites"},
})
if err != nil { return err }
graph := linked.Definitions["composites:or"]
runtime, err := graph.Instantiate(distributed.Implementations())
```

`Link` resolves local names and bindings while retaining composition references (`Stage: "linked"`). `Flatten` expands finite graph references into one Petri net and clears that stage marker. `Compose` joins declarations and flattens. A reference selects its graph explicitly, with an optional explicit namespace relocation. Without that relocation it retains the target graph's names; instances never acquire automatic isolation. Bindings override individual names. Equal resolved names unify compatible declarations; conflicts halt linking. Different names remain independent. Recursive references are rejected.

Transition inputs/outputs are directed arcs to declared places. Place-to-place and transition-to-transition arcs are rejected. Canonical places, transitions, endpoints/ports and composition references are sorted; provenance remains separate from semantics. Explicit duplication supplies token fan-out. OR and MUX use ordinary graph composition, with no special evaluator. See [distributed authoring](docs/linker.md).

### Consumer-owned implementations

Meshwork does not own application implementations. The distributed example models the intended boundary: [graph.nand.meshwork.yaml](examples/distributed/primitives/implementation/graph.nand.meshwork.yaml) and its Go implementation [logic.nand.go](examples/distributed/primitives/implementation/logic.nand.go) live together in the consuming project. The project-owned [registry](examples/distributed/implementations.go) explicitly binds `logic.nand.go` to its Go function before execution. Meshwork neither imports consumer packages nor dynamically loads source files.

## Firing and time

A place holds zero or one immutable typed token. Starting an attempt atomically reserves every input and locks every output while preserving the actual marking. Output locks block producers without creating tokens or disabling consumers of actual unreserved tokens. Success atomically consumes inputs and produces outputs. Failure, panic, cancellation, invalid output or timeout releases reservations/locks and preserves input marking, then calls optional `OnError`.

Omitted transition timeout (`0` in Go, omitted or `infinite` in YAML) imposes no Meshwork deadline. Finite deadlines use an injected `Clock` with stoppable timers. `RealClock` uses elapsed real time; `ManualClock` advances only through `Advance`. `Runtime.Start` returns an attempt ID and completion handle; `Fire` starts and waits. A finalized timeout cannot commit a late result. The provisional deterministic convention is **deadline equality wins over completion**. External work may keep running despite cancellation, and its side effects cannot be rolled back by marking atomicity.

`TimeoutControlGraph` and `DelayGraph` use ordinary typed places/transitions and a clock capability token. `TimingImplementations` resolves the token `execution` to the runtime's injected clock. Optional attempt supervision registers a commit deadline before waiting. See [clocks, timeout control and manual scenarios](docs/timing.md), including the executable [timing fixture](examples/timing/tests.scenarios.timeout.meshwork.yaml).

Tokens hash a type tag, separator and normalized JSON with SHA-256. Values are copied and validated at API boundaries. Types are nominal tags, without a schema registry. Numeric spellings such as `1` and `1.0` remain distinct.

## GraphML and semantic comparison

GraphML enters the existing linker through explicit `meshwork.*` properties, including `meshwork.timeout`. Optional `--yed` shape/label conventions support manual authoring. XML IDs, layout, grouping and original XML remain separate from computation. Discovery is opt-in.

```powershell
go run ./cmd/meshwork import ./linker/testdata/graphml/graph.nand.graphml
go run ./cmd/meshwork validate ./linker/testdata/graphml/graph.nand.graphml
go run ./cmd/meshwork import ./linker/testdata/graphml/graph.nand.graphml --format yaml
go run ./cmd/meshwork diff ./linker/testdata/graphml/graph.nand.graphml ./linker/testdata/graphml/graph.nand.meshwork.yaml
go run ./cmd/meshwork diff ./linker/testdata/graphml ./linker/testdata/graphml --format json
```

Diff compares identities, types, implementations, timeouts, directed arcs and local ports, plus references/bindings at the linked stage. Mixed resolution stages are rejected. The CLI compares flattened inputs; Go callers can compare linked graphs. Semantic changes, source-provenance changes and conservative rename suggestions remain separate. Presentation-only edits have no semantic effect. See [import and comparison details](docs/linker.md).

## Remaining boundaries

Default runtime error-handler policy and the final deadline tie convention remain product decisions. Scenarios fire each transition at most once. Manual scenarios require clock waits to use `WaitClock` so due work can settle before assertions; arbitrary blocking adapters require explicit cancellation. General cyclic/reactive scheduling, immutable revisions, cross-language numeric normalization and place-selection traversal remain open. Next smallest useful milestone: explicit expected-failure assertions in scenarios.

GraphML XML rewriting/synchronization, distributed scheduling, automatic retries, promise resolution, cloud adapters, UI and Collator remain outside this milestone. Actual yEd save/reopen has not been checked.
