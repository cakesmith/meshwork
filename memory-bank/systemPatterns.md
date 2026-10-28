# System Patterns

## Files and relationships
- `meshwork/core.go`: tokens, typed places, runtime marking, reservations/locks, atomic Fire, and copied token/snapshot inspection.
- `meshwork/graph.go`: Graph, Entity, Architecture, Reference, Library, ExecutableGraph; Resolve, Compose, Flatten, Instantiate.
- `meshwork/logic.go`: reusable NAND/duplication primitives and composed NOT/OR architectures.
- `meshwork/*_test.go`: kernel invariants, composition validation, truth tables, and JSON round-trip execution.
- `cmd/demo/main.go`: all four OR cases using the flattened five-transition graph.

## Identity and expansion
Absolute computational names resolve unchanged; relative names resolve under an explicit namespace. Malformed names are rejected, never cleaned. Explicit reference names define scopes for relative internal names; absolute names and bindings support sharing beyond containment. Equal resolved names identify one compatible place or atomic transition. Incompatible declarations or node-kind collisions fail. Definitions/architecture references are metadata, never computational edges.

Entities have ordered, nominally typed input/output places. Architectures choose exactly one atomic implementation key or graph. Every port requires a matching binding to a place explicitly declared in the referring graph. Composite implementations explicitly declare all interface places. Expansion maps those places to outer identities, checks recursion, merges compatible declarations, sorts output, and retains source definition paths. Composite timeouts are rejected pending a semantics-preserving design. The milestone uses finite acyclic references; immutable definition revisions are not yet implemented.

Serialized executable graphs contain typed places, transition endpoints, implementation keys, local port mappings, timeouts, and source metadata. Instantiate validates topology through the kernel and binds separate Go functions using entity port names. It copies graph slices so later edits do not change an existing runtime. Only place-to-transition and transition-to-place arcs are representable.

## Firing and tokens
Fire acquires all input reservations and output locks atomically, preserves the actual marking during execution, executes outside the mutex, validates every output, and atomically commits or releases. Full output places block initial acquisition unless they are also this firing's input. Output locks block producers, not consumers of actual unreserved tokens. Failed attempts never consume inputs or produce normal outputs. Optional OnError runs after release; configure it before concurrent firing.

Execution runs in a goroutine with a buffered completion channel. Cancellation/deadline ends logical authority without waiting for underlying work; late completion cannot reach the commit path. Panics become errors and release reservations. Adapters own stopping physical work and reconciling external effects; no automatic retry policy.

Token identity is SHA-256 over type + zero byte + normalized JSON. NewToken sorts object keys via decode/remarshal with UseNumber; numeric spelling is preserved. Put/output validation rejects tampered or noncanonical bytes/hashes. Types are nominal strings, not schemas. Runtime boundaries copy token bytes. No persistent token store exists.

## Change rules and open decisions
Read activeContext and relevant technical files before each request. Update this file/techContext before architectural changes and activeContext at closure. Extend semantic regression tests when changing identity, typing, reservations, failure, timeout, or firing.

Open: place-based traversal with transition closure and typed boundaries; immutable definition revision scheme; cross-language numeric canonicalization; default runtime error policy; composite deadlines. Subgraph and slice are one concept. No cloud, GraphML round-tripping, UI, or Collator implementation in this milestone.
