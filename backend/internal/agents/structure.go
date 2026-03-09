package agents

import "fmt"

// StructureAgent builds the config for the Architecture + Lint specialist.
// It receives full repo tree structure, dependency direction graph, and interface definitions.
func StructureAgent(base AgentConfig) AgentConfig {
	base.Type = AgentStructure

	multiRepoBlock := ""
	if base.MatchSummary != "" {
		multiRepoBlock = fmt.Sprintf("\n[CROSS-REPO MATCHES]\n%s\n", base.MatchSummary)
	}

	base.SystemPrompt = StructureSystemPrompt + fmt.Sprintf(sharedRules, multiRepoBlock)
	return base
}
