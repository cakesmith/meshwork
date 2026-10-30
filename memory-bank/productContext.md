# Product Context

Graphs define computation and select exact child graphs/transition implementation keys. Scenarios select a graph, initial typed markings, clock and expectations; they cannot substitute implementations, change types, introduce transitions/arcs or rewrite composition. Deployment inputs may vary, while deployment computation is an ordinary graph.

Authoring workflow: configure repositories -> discover graph/scenario files -> resolve namespaces and explicit bindings -> unify compatible equal computational names -> validate -> flatten -> bind implementation functions -> execute with an isolated marking. Only graph and scenario file types exist. Graphs can overlap or share objects across files/repositories; no shared graph ID or automatic instance isolation is required.

Application ownership stays outside Meshwork: each transition implementation source lives beside its graph YAML in the consuming project, and that project provides the explicit registry used at execution. The implementation name in YAML selects the project registry entry; it is not a Meshwork package import or dynamic source-loading instruction.

GraphML imports enter the same linker. Explicit semantic properties or opt-in yEd conventions define computation; grouping, XML IDs, layout and source provenance are separate. Incomplete drafts can be inspected/diffed but cannot execute. Conflicts return no partial linked result and never modify source files.

Semantic diff compares graphs at one resolution stage, ignoring presentation. Separate deterministic semantic/provenance reports and conservative same-file source-node rename suggestions support review without XML writing.

Runtime execution consumes tokens; explicit duplication provides fan-out. Output locks never act like promised tokens. Failure or timeout preserves input marking and releases reservations/locks. External effects cannot be rolled back. Graph-defined deadlines default to infinite; scenarios can inject manual clocks and supply Duration/Clock tokens to ordinary timeout-control graphs.

Manual scenarios process due work and runnable transitions before assertions. Steps can inspect actual marking at 29, 30 and 31 seconds without waiting for elapsed wall time. Each transition fires at most once; the last explicit step stops the run and releases pending reservations. Default error handling and broader scheduling remain product decisions.

For shell/process composition, the persistent adapter treats graph topology as its boundary: source places are writable ingress and sink places are drained egress. NDJSON frames identify the selected place and JSON value. A parent process owns service lifetime; EOF leaves the session running and no network listener is implied.
