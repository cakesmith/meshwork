# Project Brief

## Purpose
Meshwork is a general-purpose computational platform built from typed, strictly 1-safe Petri nets. Its kernel consists of graph, marking, and firing rule. Computational semantics remain independent of deployment technology and of the first planned reference application, Collator.

## Current scope
- Standard-library Go kernel with immutable typed content-addressed tokens.
- Atomic all-input reservation/output locking and consuming marking commits.
- Deterministic resolved computational names; compatible equal names share identity.
- JSON-serializable graphs, reusable entities/architectures, explicit bindings, finite flattening, and source traceability.
- NAND, explicit duplication, NOT-from-NAND, and OR-from-NAND; verify all Boolean combinations and semantic invariants.

## Deferred scope
Place-selection traversal, immutable definition revision storage, cross-language numeric canonicalization, graph import/export, durable storage, general scheduling, distributed execution, UI, and Collator. No special promises, read arcs, retries, or backoff in the kernel.
