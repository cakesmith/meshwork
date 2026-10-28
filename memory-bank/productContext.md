# Product Context

Meshwork aims to describe, compose, compile, execute, observe, and evolve software through one computational graph model. Collator will eventually reconstruct books using Meshwork, but book-specific functionality must remain separate and waits until composition/execution foundations are sound.

The current developer workflow is Go definitions -> explicit name/port bindings -> validated flattened graph -> implementation registry -> runtime -> seed marking -> fire transitions -> inspect tokens/snapshots. Graph serialization excludes functions and layout. The OR demo exercises reusable architectures, rather than hardcoding an OR implementation.

Inputs are consumed, never implicitly read or shared as electronic signals. Explicit duplication produces tokens in separate 1-safe places. Failed attempts preserve their original marking; runtime errors are reported through an optional callback. Arbitrary external effects cannot be rolled back by marking atomicity.

Subgraphs are graphs, and slice means subgraph; no separate Slice semantics. Future authoring prioritizes yEd/GraphML, with Mermaid as another external representation. Canonical graph semantics must stabilize before full layout-preserving round-tripping.
