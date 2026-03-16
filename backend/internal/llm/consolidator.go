package llm

import (
	"code-review/backend/internal/agents"
	"code-review/backend/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RunConsolidation sends all specialist agent results to the LLM for intelligent
// consolidation. The LLM can detect semantically similar issues with different
// wording, assess comment quality, and resolve conflicts between agents.
//
// Falls back to DeterministicConsolidate on any LLM failure.
func RunConsolidation(
	ctx context.Context,
	provider LLMProvider,
	agentResults []agents.AgentResult,
	maxComments int,
) *models.ReviewResult {

	if maxComments <= 0 {
		maxComments = agents.DefaultMaxComments
	}

	// Build the consolidation prompt
	prompt := buildConsolidationPrompt(agentResults)
	systemPrompt := agents.BuildConsolidatorSystemPrompt(maxComments)

	fmt.Printf("🔄 [Consolidator] Sending %d agent results to LLM for intelligent consolidation\n",
		len(agentResults))
	fmt.Printf("   📏 [Consolidator] Prompt size: %d chars (~%d tokens)\n",
		len(prompt), len(prompt)/4)

	req := GenerateRequest{
		SystemPrompt: systemPrompt,
		Messages: []Message{
			{
				Role: "user",
				Parts: []Part{
					{Text: prompt},
				},
			},
		},
		Temperature:  0.0,
		ResponseJSON: true,
	}

	start := time.Now()
	resp, err := provider.GenerateContent(ctx, req)
	elapsed := time.Since(start)

	if err != nil {
		fmt.Printf("   ⚠️ [Consolidator] API error: %v — falling back to deterministic\n", err)
		return agents.DeterministicConsolidate(agentResults, maxComments)
	}

	if resp.FinishReason == "EMPTY" || resp.Text == "" {
		fmt.Printf("   ⚠️ [Consolidator] Empty response — falling back to deterministic\n")
		return agents.DeterministicConsolidate(agentResults, maxComments)
	}

	// Parse the JSON response
	var result models.ReviewResult
	cleaned := strings.TrimSpace(resp.Text)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		fmt.Printf("   ⚠️ [Consolidator] JSON unmarshal error: %v — falling back to deterministic\n", err)
		fmt.Printf("   Raw response: %.500s\n", cleaned)
		return agents.DeterministicConsolidate(agentResults, maxComments)
	}

	fmt.Printf("   ✅ [Consolidator] LLM consolidation complete: %d comments (%.1fs, %d→%d tokens)\n",
		len(result.Comments), elapsed.Seconds(),
		resp.InputTokens,
		resp.OutputTokens)

	return &result
}

// buildConsolidationPrompt formats all agent results into a structured prompt
// for the consolidator LLM.
func buildConsolidationPrompt(results []agents.AgentResult) string {
	var b strings.Builder

	b.WriteString("Below are the findings from 3 specialist code review agents.\n")
	b.WriteString("Consolidate them according to your instructions.\n\n")

	for _, r := range results {
		if r.Error != nil {
			b.WriteString(fmt.Sprintf("=== AGENT: %s ===\n", r.Agent))
			b.WriteString(fmt.Sprintf("ERROR: %v\n\n", r.Error))
			continue
		}

		b.WriteString(fmt.Sprintf("=== AGENT: %s (%d findings) ===\n", r.Agent, len(r.Comments)))
		if r.Summary != "" {
			b.WriteString(fmt.Sprintf("Summary: %s\n", r.Summary))
		}

		if len(r.Comments) == 0 {
			b.WriteString("No issues found.\n\n")
			continue
		}

		// Serialize each comment
		for i, c := range r.Comments {
			b.WriteString(fmt.Sprintf("\nFinding %d:\n", i+1))
			b.WriteString(fmt.Sprintf("  File: %s\n", c.File))
			b.WriteString(fmt.Sprintf("  Line: %d\n", c.Line))
			b.WriteString(fmt.Sprintf("  Severity: %s\n", c.Severity))
			b.WriteString(fmt.Sprintf("  Layer: %s\n", c.Layer))
			b.WriteString(fmt.Sprintf("  Message: %s\n", c.Message))
		}
		b.WriteString("\n")
	}

	return b.String()
}
