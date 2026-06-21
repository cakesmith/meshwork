# Meshwork

Meshwork is a minimal Petri-net execution scaffold for building, running, and debugging typed token flows. The first version focuses on:

- YAML-authored graphs
- Typed places, ports, and arcs
- Place-level boundary protocols for `stdin`, `stdout`, and `http`
- External transition logic files with executor dispatch by file type
- A small CLI for validation, simulation, and scenario checks

## Commands

Use the repo-local build helper so Gradle runs through the checked-in wrapper with a repo-local cache:

```powershell
.\build.ps1 test
.\build.ps1 run --args="--help"
.\build.ps1 run --args="validate examples\graphs\uppercase.yaml"
.\build.ps1 run --args="run examples\graphs\uppercase.yaml --trace"
.\build.ps1 run --args="run examples\graphs\uppercase.yaml --flamegraph build\metrics\uppercase.folded"
.\build.ps1 run --args="run examples\graphs\uppercase.yaml --initial-file override.yaml"
.\build.ps1 run --args="run examples\graphs\uppercase.yaml --server"
.\build.ps1 run --args="run examples\graphs\uppercase.yaml --server --http -http --http-port 8080"
.\build.ps1 run --args="scenario examples\scenarios\uppercase_scenario.yaml"
.\build.ps1 run --args="scenario examples\scenarios\fanout_object_scenario.yaml"
```

The CLI help text is generated from the command definitions, so `meshwork --help`, `meshwork run --help`, and running the CLI with no arguments all stay in sync automatically.

## Place Protocols

Places can opt into boundary protocols directly:

```yaml
places:
  - id: source
    type: STRING
    protocol:
      kind: http
      path: /ingest
      awaitOutputs: [result]
  - id: result
    type: STRING
    protocol:
      kind: http
```

Protocol rules:

- Unconfigured source-only places default to `stdin` in server mode.
- Unconfigured sink-only places default to `stdout` in server mode.
- `--http` or `-http` overlays `http` onto every unconfigured place.
- Explicit place protocol config wins over inferred defaults or the HTTP overlay.

Server mode behavior:

- Single `stdin` place: one non-empty input line becomes one token.
- Multiple `stdin` places: one JSON object per line shaped like `{placeId:[tokens...]}`.
- Single `stdout` place: one output token per line.
- Multiple `stdout` places: one JSON object per event shaped like `{placeId:[tokens...]}`.
- HTTP `POST` injects tokens into an HTTP-bound place.
- HTTP `GET` drains queued tokens from an HTTP-bound place.
- If an HTTP input place declares `awaitOutputs`, `POST` waits up to `--timeout-ms` for those places and returns their drained tokens.

## External Logic

Transitions reference external logic files through graph executor config. There are now two execution lanes:

- `kotlin-process`: compatibility and prototyping lane for `.kts` logic
- `process`: generic wrapper-driven lane for repo-local scripts, long-lived workers, compiled jars, or native binaries

`kotlin-process` logic still exposes:

```kotlin
fun run(input: String): Any?
```

The runtime handles contract encoding:

- `text`: ordered port values over stdin/stdout as newline-delimited text
- `json`: named inputs/outputs as JSON text

If a single output port fans out to multiple places, the script can still return one value to duplicate everywhere, or return an object keyed by destination place id. See `examples/graphs/fanout_object.yaml` and `examples/scenarios/fanout_object_scenario.yaml`.

### Generic Process Wrappers

`process` executors use a wrapper block:

```yaml
execution:
  defaultExecutor: process-wrapper
  executors:
    - id: process-wrapper
      kind: process
      extensions: [".logic"]
      wrapper:
        command: ["powershell", "-ExecutionPolicy", "Bypass", "-File", "wrapper/echo.ps1", "{logicPath}"]
        mode: PERSISTENT
        timeoutMs: 30000
```

Useful command placeholders:

- `{logicPath}`: resolved path to the transition logic file
- `{projectRoot}`: normalized project root
- `{java}`: current JVM binary
- `{classpath}`: current Meshwork classpath

The generic process worker protocol is one compact JSON line per request and response:

- Request: `{"id":"...", "inputs":{...}}` for JSON contracts
- Response: `{"id":"...", "outputs":{...}}`

For hot paths, prefer one of these:

- `wrapper.mode: PERSISTENT` for long-lived interpreter or service workers
- compiled jars/binaries behind the same `process` wrapper shape when startup cost matters more than scripting convenience

The intended path is: prototype in `.kts`, then promote frequently invoked logic to a persistent worker or compiled wrapper when latency matters.

## Metrics

Transition traces now include `durationMs=...` for each firing. For reusable outputs, configure metrics plugins under `execution.metrics`:

```yaml
execution:
  metrics:
    plugins:
      - kind: prometheus
        path: /metrics
      - kind: flamegraph
        output: build/metrics/transitions.folded
```

Current built-ins:

- `prometheus`: exposes scrape-friendly text on the existing server-mode HTTP listener
- `flamegraph`: writes folded-stack samples that tools such as `flamegraph.pl` can render later

You can also force a one-off folded output from the CLI with `run --flamegraph <repo-relative-path>`.
