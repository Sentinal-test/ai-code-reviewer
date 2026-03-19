package agents

import (
	"fmt"
	"strings"
)

func BuildSpecialistSystemPrompt(basePrompt, multiRepoBlock string) string {
	return strings.TrimSpace(basePrompt + "\n\n" + fmt.Sprintf(sharedRules, multiRepoBlock))
}

func BuildConsolidatorSystemPrompt(maxComments int) string {
	// Consolidator has a fixed, high-security prompt defined in prompts.go.
	return fmt.Sprintf(ConsolidatorSystemPrompt, maxComments)
}
