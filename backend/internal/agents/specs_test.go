package agents

import (
	"strings"
	"testing"
)

func TestBuildSpecialistSystemPrompt_DoesNotInlineAgentDocs(t *testing.T) {
	prompt := BuildSpecialistSystemPrompt("correctness", CorrectnessSystemPrompt, "")

	if strings.Contains(prompt, "AGENT IDENTITY REFERENCE") {
		t.Fatalf("specialist prompt should not inline agent identity docs")
	}
	if strings.Contains(prompt, "AGENT WORKFLOW REFERENCE") {
		t.Fatalf("specialist prompt should not inline workflow docs")
	}
	if !strings.Contains(prompt, "MANDATORY REVIEW RULES") {
		t.Fatalf("specialist prompt should include shared runtime rules")
	}
}
