# Product Context

## Why This Exists
Meshwork is intended to make temporal coordination and data flow explicit through a Petri net substrate. Typed places and arcs provide a way to validate token movement, while explicit transition logic keeps compute flexible.

## User Workflow
1. Author a graph in YAML.
2. Attach transition logic through external files.
3. Validate graph structure and type compatibility.
4. Run the graph with optional tracing for debugging.
5. Capture small end-to-end checks with scenario files.

## Design Intent
- Minimal scaffolding first
- Explicit data flow over hidden runtime magic
- Flexible compute boundaries through executor abstraction
- Easy future expansion to new external logic file types

