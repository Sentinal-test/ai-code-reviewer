package agents

import (
	"fmt"
)

// CorrectnessAgent builds the config for the Bugs + Performance specialist.
// It receives data flow edges, function call chains, and type definitions from the code graph.
func CorrectnessAgent(base AgentConfig) AgentConfig {
	base.Type = AgentCorrectness

	multiRepoBlock := ""
	if base.MatchSummary != "" {
		multiRepoBlock = fmt.Sprintf(`
RULE 0 — MULTI-REPO / MICROSERVICES CONTEXT (Active for this review):
  This review spans MULTIPLE REPOSITORIES in a microservices or multi-repo architecture.
  Code in THIS repo may depend on — or be depended upon by — code in OTHER repos.
  The matched cross-repo files below show related code from sibling repositories.

  USE THIS CONTEXT TO DETECT CROSS-REPO ISSUES such as:
  • API contract mismatches — a service changed its request/response shape but consumers
    in other repos still expect the old format (wrong fields, types, status codes, URLs).
  • Shared model/type drift — a data model, enum, or constant is defined in one repo and
    used by others; a change here silently breaks downstream consumers.
  • Event/message schema changes — producers changed event payloads or topic names without
    updating subscribers in other repos.
  • Cross-service error handling — a service changed its error codes/formats but callers in
    other repos still parse the old format.
  • Dependency version conflicts — shared libraries updated in one repo but pinned to an
    older version in another, causing runtime incompatibilities.
  • Configuration/environment mismatches — env vars, feature flags, or config keys renamed
    or removed in one repo but still referenced by another.
  • Database/migration conflicts — schema changes in one service that break queries or
    assumptions in another service sharing the same database.
  • Authentication/authorization flow breaks — auth token formats, header names, or
    middleware behavior changed in one service but not updated in others.

  IMPORTANT: Only flag cross-repo issues when you can identify a CONCRETE mismatch or
  breakage from the match data below. Do NOT speculate about hypothetical problems.

  [CROSS-REPO MATCHES]
%s
`, base.MatchSummary)
	}

	base.SystemPrompt = BuildSpecialistSystemPrompt("correctness", CorrectnessSystemPrompt, multiRepoBlock)
	return base
}
