# Meshwork

A standard-library Go prototype for strictly 1-safe, typed Petri nets. The current milestone composes OR from reusable NAND architectures through explicit token fan-out. Meshwork remains independent of Collator.

## Run

Go 1.23 or later:

```powershell
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/demo
```

The demo runs all four OR inputs. Each execution fires two duplication transitions and three NAND transitions. For example, `true OR false = true (5 transitions)`. The race detector requires a supported C compiler.

If sandbox permissions prevent writing the default Go build cache, use a writable cache before these commands:

```powershell
$env:GOCACHE = Join-Path $env:TEMP 'meshwork-go-build'
```

## Representation and composition

- `meshwork/core.go`: typed places, immutable content-addressed tokens, marking, and atomic firing.
- `meshwork/graph.go`: graph definitions, entities, architecture references, deterministic name resolution, composition, and flattening.
- `meshwork/logic.go`: NAND and duplication primitives; NOT and OR composed as ordinary graphs.
- `cmd/demo`: executes the flattened OR graph for all four input combinations.

`Graph` declares typed places and named architecture references with explicit port bindings. An `Entity` declares ordered typed inputs/outputs. An `Architecture` implements an entity using either a Go implementation key or another graph. Functions live in a separate implementation registry; graphs, libraries, and flattened executable graphs serialize with `encoding/json`.

```go
library, implementations := meshwork.LogicLibrary()
flat, err := library.Compose("/logic", meshwork.ORGraph())
if err != nil { return err }
runtime, err := flat.Instantiate(implementations)
```

`Compose` joins graph declarations and flattens them. Relative names resolve under the supplied namespace; absolute names stay absolute. Each explicitly named reference supplies the scope for its internal relative names. Bindings connect its interface places to declared, type-compatible places in the referring graph. Internal elements can use absolute names to share identities across scopes. File layout has no role in identity.

Equal resolved names unify compatible place or atomic transition declarations. Different names stay independent. Conflicts, malformed names, duplicate arcs, missing bindings, incompatible types, missing implementations, and recursive architecture expansion are errors. No place-to-place or transition-to-transition arc can be represented: transition endpoints must name declared places. Flattened places/transitions and definition trace paths are sorted, independent of declaration order. Source paths are metadata, never computational edges.

NOT consumes one input through a duplicator, producing equal immutable token references in two distinct places. NAND then consumes both. OR composes two such NOT architectures and one final NAND. Only the NAND primitive evaluates a Boolean expression; OR is graph composition. The example's firing loop is limited to this acyclic graph and is not a general scheduler.

## Firing guarantees

A place holds zero or one token reference. Every attempt atomically reserves all inputs and locks all outputs while preserving the marking. Competing consumers cannot reserve those inputs, and competing producers cannot claim locked outputs. Output locks alone never enable consumers. An actual unreserved input is consumable regardless of its output lock.

Success atomically consumes all inputs and produces all outputs. Failure, panic, cancellation, invalid output, or finite timeout releases reservations without committing that attempt, then calls optional `OnError`. A self-loop consumes and replaces its own token. Initially full output places block producers, except for a transition's own input/output self-loop. No partial reservation is acquired.

Omitted atomic timeout (`0`) means infinite. Finite timeout revokes commit authority and returns even if underlying work ignores cancellation; late results cannot commit. Such work may continue running and retain a goroutine. Adapters/implementations remain responsible for stopping work and reconciling external effects. Retry/backoff is not a kernel policy. Set `OnError` before concurrent execution; no default handler policy has been selected.

Tokens hash a type tag, a zero separator, and normalized JSON using SHA-256. Object keys are sorted, including struct/RawMessage input, and bytes are copied at API boundaries. `Put` and firing validate canonical bytes/hash and declared token type. Types are nominal tags, not JSON schema validators. Numbers preserve their JSON spelling (`1` and `1.0` differ); cross-language numeric canonicalization remains open. New object normalization can change older struct token hashes; this prototype has no persistent store to migrate.

## Open boundaries and next step

Place-based subgraph traversal/closure policies, immutable definition revision storage, language-independent token canonicalization, and default error handling remain undecided. Composite architecture timeouts are rejected: lowering a whole-graph deadline to individual transitions would change semantics. Entities currently require nonempty inputs and outputs with distinct port names; self-loops bind separate input/output ports to the same place.

Next smallest milestone: specify and test place-based subgraph selection with transition closure and typed boundaries. There is no separate Slice model. GraphML/yEd layout round-tripping, Mermaid import/export, cloud adapters, persistence, UI, and Collator are deferred.
