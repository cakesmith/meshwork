# System Patterns

## Architecture
The initial scaffold is a single Kotlin/JVM module with these responsibilities:

- CLI: declarative command and option specs for `validate`, `run`, and `scenario`, with a shared parser and auto-generated help text
- Parser functions: read graph and scenario YAML into source models
- Validator: checks graph ids, references, contracts, and type compatibility
- Simulator: executes enabled transitions against token state
- Executor runtime: resolves transitions to one-shot or persistent external-worker sessions and reuses workers for the lifetime of one run or server session
- Server session: keeps one mutable marking alive for protocol-driven `run --server` sessions
- Process executor: launches repo-provided wrapper commands for generic external logic
- Kotlin process worker: child JVM entrypoint that evaluates `.kts` files and now speaks the same request/response protocol as generic workers
- Metrics runtime: records per-transition execution duration from one timing hook around external executor calls and fans those events out to configured metrics plugins

## Graph Model
- Places own token queues and declare a type.
- Places can also declare an optional `protocol` block with `kind`, optional HTTP `path`, and HTTP `awaitOutputs`.
- Executors can now declare an optional `wrapper` block with `command`, `mode`, `timeoutMs`, and optional `handshake`.
- Transitions declare typed input ports and output ports.
- Arcs connect places to transition input ports or transition output ports to places.
- Every transition references external logic metadata through a `logic` block.

## Execution Model
- Graph execution configuration provides executor registrations and a default executor id.
- Graph execution can also declare an optional `metrics.plugins` list so timing outputs stay config-driven.
- A transition can override executor id and contract mode.
- The runtime resolves logic file paths relative to the project root and rejects absolute paths.
- `process` executors are wrapper-driven and default to the JSON contract when no contract is set.
- Persistent worker reuse is keyed by executor id plus logic path and is scoped to one batch run or one server session; there is no cross-invocation daemon.
- CLI-provided initial marking overrides reuse the same `Map<String, List<JsonNode>>` shape as graph `initialMarking` and scenario `inputs`, and can be loaded from YAML or JSON text.
- In server mode, unconfigured boundary places infer protocols from topology: source-only places become `stdin`, sink-only places become `stdout`, and `--http` can overlay `http` onto every unconfigured place.
- `text` contract uses ordered newline-delimited stdin/stdout.
- `json` contract uses named inputs and outputs in JSON text.
- Generic and Kotlin workers both speak one-line request/response JSON envelopes; JSON-contract requests carry `inputs`, JSON-contract responses carry `outputs`, and response ids must round-trip unchanged.
- Kotlin scripts still expose `fun run(input: String)`, but the worker now adapts between the stable worker envelope and the legacy script input/output shape so existing `.kts` logic keeps working.
- If an output port fans out to multiple destination places, the runtime can either duplicate one returned value to every destination or, when the returned value is an object keyed by destination place id, route each field to its matching place.
- The runtime encodes typed port values into text or JSON before process execution and validates typed outputs after execution.
- Server mode serializes event handling behind one lock, runs each injected event until idle, drains `stdout` places after each event, and exposes HTTP places through JDK `HttpServer` contexts.
- HTTP places support `POST` to inject tokens and `GET` to drain queued tokens. If an input place declares `awaitOutputs`, the POST waits up to `timeout-ms` for those output places and returns only the newly produced tokens for that request window.
- Metrics capture is transition-scoped: every firing reports transition id, executor id, logic path, duration, and success/failure.
- Prometheus metrics should piggyback on the existing server-mode `HttpServer` instead of starting a second listener.
- Flame graph output should stay file-based and emit folded-stack lines so external flame graph tooling can render it later.

## Extension Pattern
New logic runtimes should fit behind the generic `process` wrapper path first; only add built-in runtime-specific behavior when the shared worker protocol is not sufficient.
New metrics collectors should fit behind the same small plugin event sink shape first; only add a broader plugin system if two collectors need incompatible lifecycles.
