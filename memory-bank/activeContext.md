# Active Context

## Current Phase
Bootstrap complete with protocol-driven server mode, generic logic adapters, and transition metrics plugins

## What Was Added
- Single-module Kotlin/JVM scaffold with Gradle build files, repo-local `build.ps1`, and generated wrapper support
- YAML graph and scenario parsing for places, transitions, arcs, executor config, initial marking, and expectations
- Graph validation for duplicate ids, missing references, type mismatches, executor wiring, and repo-relative logic paths
- In-memory simulator with typed token queues, step execution, and optional trace output
- External logic execution through executor dispatch, with `kotlin-process` as the first runtime and `.kts` support
- Generic `process` executors with wrapper blocks, JSON request/response envelopes, and repo-local command placeholders such as `{logicPath}` and `{classpath}`
- Session-scoped executor pooling so persistent workers are reused across one batch run or one `run --server` lifetime instead of respawning on every transition firing
- A rewritten Kotlin worker protocol so `.kts` remains compatible while sharing the same worker envelope and persistent-session machinery
- CLI commands for `validate`, `run`, and `scenario`
- Declarative CLI command/option specs with auto-generated general and per-command help text, plus `run --initial-file` / `-i` for YAML-or-JSON initial marking overrides from a repo-relative file or stdin
- Fanout-aware transition output routing: Kotlin scripts can now return structured objects, and when one output port fans out to multiple places the runtime can route object fields by destination place id instead of cloning one scalar everywhere
- Place-level protocol config on graph places with `stdin`, `stdout`, and `http`, plus HTTP `path` and `awaitOutputs`
- A long-running `run --server` mode that resolves default boundary protocols from graph topology, supports `--http` / `-http` overlay, and serves HTTP places through the JDK HTTP server
- Mutable server sessions over the existing simulator so stdin and HTTP events reuse one marking, run until idle, and drain protocol-bound outputs after each event
- Validation for place protocol shape, duplicate HTTP paths, and unknown awaited output places
- Validation for executor wrapper shape, positive timeouts, and defaulting `process` executors to JSON when no contract is declared
- Transition metrics plugins with one timing hook per firing, built-in `prometheus` scrape output, and folded flamegraph file emission
- CLI `run --flamegraph <path>` support for one-off folded flamegraph output without editing graph YAML
- Trace lines now include `durationMs=...` so transition timing is visible even without a metrics plugin
- New executor tests covering repo-local wrapper scripts, compiled wrappers, persistent worker reuse, timeout handling, and response id mismatches
- New metrics tests covering flamegraph file output and Prometheus scraping through the existing server listener
- Focused server tests covering protocol parsing, default inference, stdin/stdout server flow, HTTP async GET/POST flow, awaited HTTP outputs, and timeout behavior
- End-to-end tests plus checked-in example graphs, scenarios, and logic files
- Ponytail over-engineering review completed on 2026-06-18 with no source edits; the main follow-up is trimming duplicated build bootstrap paths and single-implementation runtime abstractions
- Ponytail whole-repo audit completed on 2026-06-18 with no source edits; the highest-value cuts are still wrapper/bootstrap duplication, one-implementation executor layers, parser wrappers, and repeated graph validation paths
- Ponytail simplification pass implemented on 2026-06-18: `build.ps1` now delegates straight to the checked-in wrapper, parser helpers are top-level functions, scenario runs reuse one validated graph, and the executor path is a single direct `kotlin-process` class instead of interface/registry/request-response layers
- Windows app-image packaging added on 2026-06-18 through a `packageWindowsExe` Gradle task that shells out to the JDK's `jpackage`

## What Now Works
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 test`
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 run --args="validate examples\graphs\uppercase.yaml"`
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 run --args="--help"`
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 run --args="run examples\graphs\uppercase.yaml --help"`
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 run --args="scenario examples\scenarios\uppercase_scenario.yaml"`
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 run --args="run examples\graphs\uppercase.yaml --initial-file override.yaml"`
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 run --args="run examples\graphs\uppercase.yaml --server"`
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 run --args="run examples\graphs\uppercase.yaml --server --http -http --http-port 8080"`
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 run --args="run examples\graphs\uppercase.yaml --flamegraph build\metrics\uppercase.folded"`
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 run --args="scenario examples\scenarios\fanout_object_scenario.yaml"`
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 test --tests dev.meshwork.MeshworkExecutorTest --tests dev.meshwork.MeshworkScaffoldTest --tests dev.meshwork.MeshworkServerTest --no-daemon`
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 test --no-daemon`
- Local Codex tooling now has `ponytail@ponytail` v`4.7.0` installed from the `DietrichGebert/ponytail` marketplace, and `node v22.12.0` is available for its lifecycle hooks
- A fresh Ponytail smoke check still passes through `powershell -ExecutionPolicy Bypass -File .\build.ps1 test` after the review
- After the simplification pass, the same test and CLI commands still pass with the shorter runtime/build path
- `powershell -ExecutionPolicy Bypass -File .\build.ps1 packageWindowsExe --no-daemon`
- `build\jpackage\meshwork\meshwork.exe validate examples\graphs\uppercase.yaml`

## Known Constraints
- `.kts` remains the easiest built-in prototyping path, but the preferred hot-path lane is now `process` with either `wrapper.mode: PERSISTENT` or a compiled wrapper command.
- The text contract is still newline-delimited and intentionally minimal; generic `process` executors default to JSON unless a transition or executor declares another contract explicitly.
- External logic execution is not sandboxed.
- `run --initial-file` accepts repo-relative paths or `-` for stdin and reuses the same `Map<String, List<JsonNode>>` shape as graph `initialMarking` and scenario `inputs`.
- `run --server` reuses stdin for live events unless `--initial-file -` already consumed it for initial marking bootstrap.
- Awaited HTTP outputs return only newly produced tokens after the request baseline; this is designed for boundary/output places, not for arbitrary internal places with heavy downstream consumption.
- Persistent workers are single-threaded and handle only one in-flight request at a time in this version.
- On multi-destination fanout, a returned object is treated as per-place routing only when its keys match destination place ids; otherwise the whole value is still duplicated to every destination.
- Prometheus metrics are exposed only when `run --server` starts the existing HTTP listener; batch runs can still emit folded flamegraph files.
- The flamegraph output is collapsed-stack text, not a rendered SVG; render it with your preferred flame graph tooling after the run.
- `build.gradle.kts` still keeps both Kotlin `jvmTarget=21` and Java `source/targetCompatibility=21`; the attempted one-declaration cleanup failed on this machine because Java 25 without a local JDK 21 makes Gradle/Kotlin target validation diverge.
- `build/libs/meshwork-0.1.0.jar` is the default Gradle `jar` output; the project is configured as an `application`, but no separate `bin` launcher is checked in unless distribution tasks such as `installDist` are used.
- The Windows packaging task is intentionally marked incompatible with Gradle's configuration cache so `jpackage` can run without extra build plumbing.
- `.\build.ps1 test` still passes on 2026-06-20, but Kotlin daemon startup hit `AccessDeniedException` under `C:\Users\nicks\AppData\Local\kotlin\daemon\...` and Gradle fell back to non-daemon compilation automatically.

## Next Steps
- Decide whether the next round should add checked-in wrapper examples under `examples/` so interpreted and compiled adapter patterns are visible without reading tests
- Decide whether `kotlin-process` should gain a config knob to default to persistent mode once teams are comfortable with stateful worker reuse
- Decide whether server-mode trace output should get a separate stderr/debug lane instead of sharing the main CLI output stream
- Decide whether to check in one or two protocol-enabled example graphs for curl/pipe demos instead of keeping the feature README-only for now
- Decide whether Prometheus output should stay summary-style or grow real bucketed histograms once scrape consumers settle
- Decide whether flamegraph output should eventually emit a rendered artifact or keep the current folded-text handoff
- Add more executor kinds only if they need behavior the shared `process` wrapper protocol cannot express cleanly
- Decide whether richer token/object typing should stay JSON-based or move to a stronger runtime schema
- Decide later whether per-place fanout routing should stay keyed by destination place id or gain an explicit wrapper to avoid ambiguity with literal object payloads
- Improve trace/debug output with per-place token snapshots and better executor stderr reporting
- In a fresh Codex thread, open `/hooks` once to review and trust Ponytail's lifecycle hooks if Codex prompts for hook approval
- If a first-class CLI install layout is wanted, wire the release flow around the `application` plugin's distribution tasks instead of renaming the plain jar output to `bin`
- If you want a single distributable installer `.exe` instead of an app-image folder, add a second `jpackage --type exe` task later
