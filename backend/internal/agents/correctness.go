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
		multiRepoBlock = fmt.Sprintf("\n[CROSS-REPO MATCHES]\n%s\n", base.MatchSummary)
	}

	base.SystemPrompt = CorrectnessSystemPrompt + fmt.Sprintf(sharedRules, multiRepoBlock)
	return base
}
