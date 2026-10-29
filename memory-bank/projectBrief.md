# Project Brief

## Requirements and scope
Meshwork is a general-purpose platform based on typed, strictly bipartite, 1-safe Petri nets. The kernel is graph, marking and consuming firing rule. Graphs are the sole reusable computational definitions; scenarios execute already-composed graphs. Adapters provide implementations without adding computation models. No separate entity, architecture, workflow, module or Slice abstraction.

Current foundation includes immutable content-addressed tokens, atomic reservation/output locking and commit/rollback, explicit graph composition and bindings, deterministic canonical linking/flattening, distributed YAML authoring, bounded GraphML/yEd import, provenance and semantic diff. NAND/duplication/NOT/OR/MUX examples remain supported.

Transition implementations belong to the consuming project, beside the graph YAML that names them. Meshwork supplies the graph/runtime contract but never imports, replaces, compiles, or discovers consumer code. A consuming project explicitly assembles its own implementation registry when it instantiates a graph.

Transition timeouts are graph definitions and default to infinite. Real/manual injected clocks, unique attempt identities, atomic deadline/commit decisions, ordinary timeout-control/delay graphs and deterministic manual-clock scenario steps are required. Deadline equality currently expires the attempt; this convention is provisional. Late results cannot commit after expiration.

## Exclusions and remaining decisions
No GraphML XML rewriting/synchronization, cloud adapters, distributed scheduler, automatic retries/backoff, arbitrary promises, new vertex kinds, UI or Collator code. Default runtime error policy and final tie-breaking convention remain undecided. General cyclic/reactive scheduling, expected-failure scenario assertions, immutable revisions, numeric normalization and place-selection traversal remain future work.
