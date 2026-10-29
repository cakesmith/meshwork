# Project Brief

## Purpose
Meshwork is a general-purpose computational platform built from typed, strictly 1-safe Petri nets. Its kernel consists of graph, marking, and firing rule. Computational semantics remain independent of deployment technology and of the first planned reference application, Collator.

## Current scope
- Deterministic canonical semantic graph diffs, independent source-provenance reports, conservative possible-rename evidence and text/JSON CLI for independently resolved YAML/GraphML files or workspaces. No XML writes; the next milestone is a non-destructive patch planner.
- Standard-library Go kernel with immutable typed content-addressed tokens.
- Atomic all-input reservation/output locking and consuming marking commits.
- Deterministic resolved computational names; compatible equal names share identity.
- JSON-serializable graphs, reusable entities/architectures, explicit bindings, finite flattening, and source traceability.
- NAND, explicit duplication, NOT-from-NAND, OR-from-NAND, and YAML MUX reusing OR/NOT/NAND; verify all Boolean combinations and semantic invariants.
- Distributed YAML graph/scenario discovery and deterministic linking across explicitly configured repositories, with resolved-name merging, conflict provenance, explicit definition reuse, canonical executable graphs, and discover/link/validate CLI commands.
- GraphML/yEd import into that same linker, explicit semantic properties, optional shape/label conventions, opt-in discovery, incomplete inspection, deterministic semantic JSON/YAML output, and separate presentation/provenance retention. Equivalent NAND sources must resolve to one canonical computational graph.

## Deferred scope
Place-selection traversal, file-level entity/architecture contract resolution, immutable definition revision storage, cross-language numeric canonicalization, bidirectional GraphML synchronization, Mermaid import/export, durable storage, general scheduling, distributed execution, UI, and Collator. No special promises, read arcs, retries, or backoff in the kernel.
