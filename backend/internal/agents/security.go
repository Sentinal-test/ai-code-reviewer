package agents

import "fmt"

// SecurityAgent builds the config for the Security specialist.
// It receives import graph context, auth function signatures, and security-focused rules.
func SecurityAgent(base AgentConfig) AgentConfig {
	base.Type = AgentSecurity

	multiRepoBlock := ""
	if base.MatchSummary != "" {
		multiRepoBlock = fmt.Sprintf("\n[CROSS-REPO MATCHES]\n%s\n", base.MatchSummary)
	}

	base.SystemPrompt = SecuritySystemPrompt + fmt.Sprintf(sharedRules, multiRepoBlock)
	return base
}
