# AI Code Reviewer Agent System

## Orchestrator
- Responsibility: Prepare each review chunk, build differentiated context, launch specialist agents, and collect their outputs.
- Spawns: `Correctness`, `Security`, and `Structure`.
- Inputs: Changed files, diff, PR context, code graph context, repository structure, cross-repo matches, developer rules, tool executor.
- Returns: Specialist results to the consolidator and the final merged review result to the caller.
- Execution model: Runs the three specialist agents in parallel for the same chunk.

## Correctness Agent
- Responsibility: Find bugs, logic errors, runtime failures, edge cases, contract violations, cross-file breakage, and concrete performance regressions.
- Spawned by: `Orchestrator`.
- Inputs: Changed files, diff, PR context, slim dependency index, tool access, optional cross-repo context.
- Returns: Structured findings with summary and JSON comments.

## Security Agent
- Responsibility: Find exploitable vulnerabilities, broken trust boundaries, auth failures, injection paths, unsafe secret handling, and cross-service security drift.
- Spawned by: `Orchestrator`.
- Inputs: Changed files, diff, PR context, slim dependency index, tool access, optional cross-repo context.
- Returns: Structured findings with summary and JSON comments.

## Structure Agent
- Responsibility: Find architecture issues, breaking contract drift, dependency direction problems, duplication, misplacement of code, and structural inconsistencies.
- Spawned by: `Orchestrator`.
- Inputs: Changed files, diff, PR context, repository structure, tool access, optional cross-repo context.
- Returns: Structured findings with summary and JSON comments.

## Consolidator
- Responsibility: Merge specialist findings into one concise, high-signal review.
- Spawned by: `Orchestrator` after all specialist agents complete.
- Inputs: All specialist summaries and findings.
- Returns: Final merged summary plus deduplicated, ranked comments.
- Constraint: Consolidates only; it does not perform primary review or invent new issues.

## Review Flow
1. Orchestrator builds one review chunk.
2. Correctness, Security, and Structure run in parallel on that chunk.
3. Each specialist returns structured findings independently.
4. Consolidator merges, deduplicates, and caps the final review output.
