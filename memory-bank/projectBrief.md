# Project Brief

## Goal
Meshwork is an extremely minimal framework for building, executing, and debugging Petri net graphs.

## Core Requirements
- Keep the initial scaffold small and easy to evolve.
- Model typed places, transition ports, and arcs so token flow can be validated.
- Support YAML-authored graphs and scenarios.
- Allow transition logic to live in external files.
- Route each external file type through an executor abstraction.
- Ship a runnable CLI for validate, run, and scenario workflows.

## Current Scope
- Single Kotlin/JVM Gradle module
- In-memory simulation
- Repo-relative external logic files
- Default text stdin/stdout contract plus JSON override
- One initial executor: Kotlin script logic in a child JVM process

## Out Of Scope For Now
- GUI tooling
- Distributed execution
- Plugin marketplaces
- Sandboxed execution
- Rich scenario DSLs

