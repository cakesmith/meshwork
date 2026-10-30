# Meshwork

Meshwork is a toolkit for authoring, executing, and deploying composable, Petri-net style graph systems.

## Use the examples

Prerequisite: Go 1.23 or later. The commands below use `go run`, so no installation is required.

### 1. Inspect the smallest graph

The distributed example starts with a NAND graph and its adjacent consumer-owned Go implementation: [graph.nand.meshwork.yaml](examples/distributed/primitives/implementation/graph.nand.meshwork.yaml) and [logic.nand.go](examples/distributed/primitives/implementation/logic.nand.go).

Validate the primitive repository without executing its implementation:

```powershell
go run ./cmd/meshwork validate ./examples/distributed/primitives
```

### 2. Execute the NAND truth table

The [NAND scenarios](examples/distributed/composites/tests/tests.scenarios.nand.meshwork.yaml) provide the four input/output cases. The composites repository declares its primitives dependency in [meshwork.repositories.yaml](examples/distributed/composites/meshwork.repositories.yaml), so the command can infer the repository root from the scenario file:

```powershell
go run ./cmd/meshwork execute --scenario ./examples/distributed/composites/tests/tests.scenarios.nand.meshwork.yaml
```

### 3. Reuse NAND to make OR

The [OR graph](examples/distributed/composites/logic/graph.or.meshwork.yaml) composes the primitive graph. Run its [truth-table scenarios](examples/distributed/composites/tests/tests.scenarios.or.meshwork.yaml):

```powershell
go run ./cmd/meshwork execute --scenario ./examples/distributed/composites/tests/tests.scenarios.or.meshwork.yaml
```

### 4. Compose the MUX

The [MUX graph](examples/distributed/composites/logic/graph.mux.meshwork.yaml) builds on the same primitives and composites. Execute its eight cases:

```powershell
go run ./cmd/meshwork execute --scenario ./examples/distributed/composites/tests/tests.scenarios.mux.meshwork.yaml
```

### 5. Run a persistent adapter

The [NAND adapter scenario](examples/distributed/composites/run.scenario.nand.meshwork.yaml) turns graph source places into NDJSON stdin and graph sink places into NDJSON stdout. Build the CLI once, then keep the process running while sending frames:

```powershell
go build -o meshwork.exe ./cmd/meshwork
.\meshwork.exe execute --scenario ./examples/distributed/composites/run.scenario.nand.meshwork.yaml
# Type {"place":"A","value":true}, then {"place":"B","value":true}.
# The process emits {"place":"/logic/nand/Q","value":false}; press Ctrl+C to stop it.
```

The adapter runs until its parent terminates it; EOF is not a stop signal. See [scenario and adapter details](docs/linker.md#scenarios).

## Check your work

```powershell
go test ./...
go vet ./...
go test -race ./...
```

If the default Go caches are unavailable, use the repository caches:

```powershell
$env:GOCACHE = Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE = Join-Path (Get-Location) '.cache/go-mod'
```

## How Meshwork works

Graphs define places, transitions, and references to other graphs; scenarios supply inputs and expectations; consuming applications supply the Go implementations. Read the [Meshwork overview](docs/overview.md) for the model and runtime, [distributed authoring and CLI reference](docs/linker.md) for graph files and commands, and [clocks and timeouts](docs/timing.md) for timed execution, and [sharing scenario plugins](docs/plugins.md) for plugin authoring.

## Remote transition executors

Graph files can declare explicit place types, local ports and arc transports. The CLI can execute a selected graph using native and remote executors under one authority:

```powershell
go run ./cmd/meshwork execute --scenario ./examples/remote/tests.scenarios.nand.meshwork.yaml --deployment ./examples/remote/deployment.yaml ./examples/remote
```

Start the executor first as described in [remote execution](docs/remote-execution.md). That guide includes the compiled Go executor, a Python interoperability example, per-transition overrides, and protocol limits.
