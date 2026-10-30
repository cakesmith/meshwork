# Places, ports and remote transition executors

The first distributed execution path uses one in-memory authority for a graph instance and independently running executor binaries. The authority reserves inputs and outputs, invokes an executor, validates its proposed result, and commits the entire firing. Executors have no API for mutating the authoritative marking. The linker and authority currently remain Go implementations; making every component independently replaceable across languages is still the broader architecture target.

## Author the graph

Existing scalar place types and endpoint-pair arcs continue to work. Places can also use `{type: TYPE}`, and arcs can explicitly specify the transition's local port and transport:

```yaml
namespace: /example
places:
  incoming: {type: Bool}
  outgoing: {type: Bool}
transitions:
  work: {implementation: effect.echo, timeout: 10s}
arcs:
  - {from: incoming, to: work, port: value, transport: http-json}
  - {from: work, to: outgoing, port: result, transport: http-json}
```

The input port is `value`; the output port is `result`. Ports are unique within each direction of a transition. Without `port`, the name defaults to the place basename after the declaring file's bindings. Reference relocation and composition preserve that local name. Same-name place declarations still merge only if compatible. Every arc must join a transition and a place; a port is not an additional vertex.

The default transport is `memory`. The other supplied transport is `http-json`. Linking preserves opaque transport identities without loading code; deployment binding rejects transports that its executor cannot implement. A transition bound to a native executor requires memory arcs; a remote executor requires HTTP/JSON on all of its arcs in this first implementation. A remote producer's output arc and a native consumer's input arc can share the same place. Different transports within one transition require a future executor with that capability and are rejected today. Changing a transport is included in graph comparison; canonical YAML now preserves explicit local ports and transports, including remapped ports.

Place token state, reservations and output locks remain in the authority's runtime instance. No token state is stored in the graph authoring file. Initial markings and expectations remain scenario data.

## Bind executors independently

A deployment file selects a default executor and optional overrides keyed by canonical transition name. Its filename and location are unrestricted; it is supplied explicitly, not discovered as a graph dependency.

```yaml
authority:
  listen: 127.0.0.1:0
default_executor: worker
executors:
  worker:
    type: http-json
    endpoint: http://127.0.0.1:8081
    token_env: MESHWORK_EXECUTOR_TOKEN
  local:
    type: native
transitions:
  /example/work: worker
```

Overrides select executors for the scenario's selected graph, so unknown transition names fail. The graph's `implementation` remains an opaque effect identity. The host validates selected executor exports, protocol, port transports and native bindings before it starts execution. A native executor uses the host-supplied implementation map; the included CLI still supplies the distributed example registry. A remote executor exports its own implementations and does not require an implementation compiled into the authority.

For different machines, bind the authority to a reachable interface and supply `authority.advertise`, for example `http://authority-host:8090`; use a fixed matching listen port. The executor endpoint must also be reachable from the authority. This is bidirectional connectivity: executors fetch inputs from the authority. The default ephemeral authority port is for same-machine use. Endpoint negotiation validates protocol/export/transport compatibility, not automatic network discovery.

## Run the compiled example

Build the authority CLI and the consumer-owned executor:

```powershell
go build -o meshwork.exe ./cmd/meshwork
go build -o executor.exe ./examples/distributed/executor
```

Set the same nonempty `MESHWORK_EXECUTOR_TOKEN` environment variable in both terminals. In the first terminal:

```powershell
.\executor.exe --listen 127.0.0.1:8081
```

In the second:

```powershell
.\meshwork.exe execute --scenario ./examples/remote/tests.scenarios.nand.meshwork.yaml --deployment ./examples/remote/deployment.yaml ./examples/remote
```

All four NAND cases should pass. The executor binary needs no graph files. It is constructed from the consumer's explicit effect registry, independent of the graph repository passed to the authority. The deployment does not launch or install remote binaries; their parent owns their lifetime.

To demonstrate another language, stop that executor and run the independent standard-library reference peer instead:

```powershell
python ./examples/remote/executor.py --port 8081
```

Run the same Meshwork command unchanged. The Python peer imports no Meshwork package; it implements the protocol below. It is an interoperability example for Boolean NAND, not a general runtime or process supervisor.

## HTTP/JSON protocol v1

Executor requests require `Authorization: Bearer TOKEN`. The token comes from the deployment's named environment variable. Run the supplied HTTP listeners only on a trusted private network, or place them behind HTTPS proxies; they do not supply TLS, sandboxing or arbitrary-source execution. The executor binary runs code selected by its owner. Input URLs are temporary bearer capabilities and must not be logged or exposed to unrelated clients.

`GET /capabilities` returns:

```json
{"protocol":"meshwork.executor.v1","transport":"http-json","implementations":["logic.nand.go"]}
```

After atomically reserving the firing's input/output places, the authority sends `POST /execute`:

```json
{
  "protocol":"meshwork.executor.v1",
  "id":"unique-invocation-id",
  "implementation":"logic.nand.go",
  "inputs":{
    "A":"http://authority:8090/inputs/unguessable-capability/0",
    "B":"http://authority:8090/inputs/unguessable-capability/1"
  }
}
```

The executor's input ports GET those URLs. Each returns a typed JSON value such as `{"type":"Bool","value":true}`. There is no additional authorization header on a capability URL. It is valid only during that invocation; removal on completion/cancellation prevents later reads. Values already fetched remain copies and convey no commit authority.

The listening executor delivers its output ports in the HTTP response to `/execute`:

```json
{"protocol":"meshwork.executor.v1","id":"unique-invocation-id","outputs":{"Q":{"type":"Bool","value":false}}}
```

All output ports are multiplexed in one response. No listener is created per token or port. Failure is `{"protocol":"meshwork.executor.v1","id":"unique-invocation-id","error":"description"}`. The authority checks correlation, types and all expected output ports before committing. A successful HTTP response does not itself commit a firing. No implicit retry is performed.

Messages are limited to 4 MiB and contain one JSON value. The Go implementation rejects unknown fields and trailing JSON. Values use JSON syntax; hashes are never supplied by executors. The authority constructs its own canonical typed tokens and hashes. Numeric equivalence across different encodings, such as `1` and `1.0`, is not normalized beyond the existing Go token rules; the example's cross-language check covers Boolean values.

## Lifecycle and limits

The authority controls graph-defined attempt deadlines and cancellation. HTTP requests are canceled when an attempt is canceled, and stale results cannot commit after finalization. An executor that ignores cancellation may continue external work; restoring the marking cannot undo those effects. Executor failures and invalid replies preserve the input marking and release reservations. No executor retry, crash recovery, durable marking, replay protection for external side effects, or authority failover is provided.

Remote v1 supports ordinary effects with a real execution clock. Manual-clock remote scenarios and the built-in timeout/delay effects are rejected because runtime clock and supervision callbacks have no remote protocol yet. Native effects retain those capabilities. Listener setup, TLS termination, artifact distribution and process restart remain deployment responsibilities.

The reusable Go entry points are `remote.LoadDeployment`, `remote.Prepare`, `Authority.NewRuntime`, `Authority.Close`, and `remote.ExecutorHandler`. Existing `plugins.BuiltIns()` and native callers still work. Scenario executors can use an explicit runtime factory through `ExecutionRequest.NewRuntime`, so the same test/stream executors operate over the authority without a second execution engine.
