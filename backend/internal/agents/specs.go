package agents

import (
	"fmt"
	"strings"
)

func buildMultiRepoHeader() string {
	return `
═══════════════════════════════════════════════════════════════════════════════
RULE 0 — MULTI-REPO / MICROSERVICES CONTEXT (Active for this review):
═══════════════════════════════════════════════════════════════════════════════
  This review spans MULTIPLE REPOSITORIES. Code in THIS repo may depend on — or 
  be depended upon by — code in OTHER repos.
`
}

func BuildCorrectnessMultiRepoBlock(matchSummary string) string {
	if matchSummary == "" {
		return ""
	}
	return buildMultiRepoHeader() + fmt.Sprintf(`
  USE THIS CONTEXT TO DETECT CROSS-REPO CORRECTNESS ISSUES:
  • API contract mismatches (wrong fields, types, status codes, URLs).
  • Shared model/type drift that breaks downstream consumers.
  • Event/message schema changes without updating subscribers.
  • Mismatched error handling or status codes between producer and consumer.
  • Database schema changes affecting queries in other services

  [CROSS-REPO MATCHES]
%s
`, matchSummary)
}

func BuildSecurityMultiRepoBlock(matchSummary string) string {
	if matchSummary == "" {
		return ""
	}
	return buildMultiRepoHeader() + fmt.Sprintf(`
  USE THIS CONTEXT TO DETECT CROSS-REPO SECURITY VULNERABILITIES:
  • Trust boundary violations where one service trusts data from another without validation.
  • Auth flow breaks across services (token format changes, header name changes).
  • Secrets or credentials shared across repos via hardcoded values.
  • SSRF where one service's interface can be used to probe internal sibling services.

  [CROSS-REPO MATCHES]
%s
`, matchSummary)
}

func BuildStructureMultiRepoBlock(matchSummary string) string {
	if matchSummary == "" {
		return ""
	}
	return buildMultiRepoHeader() + fmt.Sprintf(`
  USE THIS CONTEXT TO DETECT CROSS-REPO ARCHITECTURAL DRIFT:
  • Inconsistent patterns between services (e.g., mismatched API versions).
  • Shared library or package version drift causing runtime incompatibilities.
  • Dependency direction violations between microservices.
  • Duplicated logic/utilities that should be extracted to a shared library.

  [CROSS-REPO MATCHES]
%s
`, matchSummary)
}

func BuildConsolidatorMultiRepoBlock(matchSummary string) string {
	if matchSummary == "" {
		return ""
	}
	return buildMultiRepoHeader() + fmt.Sprintf(`
  The specialists above have analyzed these cross-repo matches. Your job is to:
  • Verify if cross-repo findings are valid and deduplicate them.
  • Ensure line numbers in cross-repo match data are not confused with [NEW_LIVE_CODE].
  • Drop any multi-repo finding that lacks a concrete mismatch evidence.

  [CROSS-REPO MATCHES]
%s
`, matchSummary)
}

func BuildSpecialistSystemPrompt(basePrompt, multiRepoBlock string) string {
	// If multiRepoBlock is provided, prepend it to sharedRules.
	// We no longer use fmt.Sprintf on the whole sharedRules to avoid fragility.
	rules := sharedRules
	if multiRepoBlock != "" {
		rules = multiRepoBlock + "\n" + rules
	}
	return strings.TrimSpace(basePrompt + "\n\n" + rules)
}

func BuildConsolidatorSystemPrompt(maxComments int, matchSummary string) string {
	if maxComments <= 0 {
		maxComments = 20 // Safe default
	}
	
	multiRepoBlock := BuildConsolidatorMultiRepoBlock(matchSummary)
	prompt := ConsolidatorSystemPrompt
	if multiRepoBlock != "" {
		// Inject it at the top of the rules
		prompt = strings.Replace(prompt, "CONSOLIDATION RULES:", multiRepoBlock+"\nCONSOLIDATION RULES:", 1)
	}

	return fmt.Sprintf(prompt, maxComments)
}
