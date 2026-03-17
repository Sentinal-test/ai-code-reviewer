# AI Code Reviewer Agent System

**Execution Model**: `Orchestrator` builds a review chunk -> spawns `Correctness`, `Security`, `Structure` in parallel -> passes results to `Consolidator` for final merge.

## Agents
- **Orchestrator**: Prepares context (diff, graphs, tool access), routes chunks, and parallelizes specialists.
- **Correctness**: Hunts runtime bugs, edge cases, logic errors, and broken contracts. Uses tools to verify intent and data flow.
- **Security**: Hunts exploitable vulnerabilities, broken trust boundaries, and missing guards. Uses tools to trace paths from source to sink.
- **Structure**: Hunts architectural drift, bad dependency direction, and broken public APIs. Uses tools to verify duplication and callers.
- **Consolidator**: Semantically deduplicates and ranks findings into a final, capped, high-signal JSON output. Does not invent issues.