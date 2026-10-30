# Sharing scenario plugins

This guide describes the current Go scenario-plugin API. [Remote transition executors](remote-execution.md) now provide an HTTP/JSON boundary for compiled effects; the broader [language-independent architecture](plugin-architecture.md) remains a target.

A scenario plugin is an ordinary Go package. It imports `plugins/api`, not the graph or application that will use it. A project can keep a plugin inside its own repository and expose its Go package to other hosts through normal Go module dependencies. Sharing that package does not require linking the project's Meshwork graph files. Graph repositories and Go plugin dependencies are configured separately; the graph linker does not discover or load Go code.

[`api.ScenarioPlugin`](../plugins/api/execution.go) supplies `Name` and `Enabled`. An executor also implements `Execute`; an observer implements `Before` and `After`. The host supplies a linked graph, scenario, implementation map, and I/O in `ExecutionRequest`. Exactly one enabled executor is required. Enabled observers run `Before` in the supplied order and `After` in reverse order. A failed `Before` stops execution; only observers whose `Before` succeeded receive `After`. Executor errors are passed to `After` and returned to the caller. An observer's `After` cannot return an error.

The [report observer](../examples/plugins/report/report.go) is independent of any target graph. A host can import it alongside either built-in executor:

```go
import (
    "context"
    "os"

    "example.com/meshwork-prototype/examples/plugins/report"
    "example.com/meshwork-prototype/plugins/api"
    "example.com/meshwork-prototype/plugins/streamexec"
    "example.com/meshwork-prototype/plugins/testexec"
)

// request contains the host's linked graph, scenario, implementations and I/O.
err := api.ExecuteScenario(context.Background(), request,
    report.Observer{Output: os.Stderr},
    testexec.Plugin{},
    streamexec.Plugin{},
)
```

The built-in test executor selects scenarios with expectations; the stream executor selects the rest and requires a stdin/stdout streaming scenario. To use another executor, supply a non-overlapping executor set: adding an always-enabled executor alongside both built-ins causes an error. [`report_test.go`](../examples/plugins/report/report_test.go) demonstrates composition with a custom executor. Executors should respect context cancellation and return errors. Keep diagnostics separate from a stream's stdout.

The existing root `plugins` package remains a compatibility facade: `plugins.BuiltIns()` and `plugins.ExecuteScenario` still work. The included CLI uses that facade and its distributed example implementation registry. A host must explicitly import and compose other plugins; neither the CLI nor scenario YAML dynamically loads them. Scenario parsing currently recognizes only the built-in stdin/stdout adapter fields, so new adapter-specific file configuration needs a separate design.
