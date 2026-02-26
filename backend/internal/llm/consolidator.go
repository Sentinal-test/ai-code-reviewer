package llm

import (
	"bytes"
	"code-review/backend/internal/agents"
	"code-review/backend/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RunConsolidation sends all specialist agent results to Gemini for intelligent
// consolidation. The LLM can detect semantically similar issues with different
// wording, assess comment quality, and resolve conflicts between agents.
//
// Falls back to DeterministicConsolidate on any LLM failure.
func RunConsolidation(
	ctx context.Context,
	client *http.Client,
	agentResults []agents.AgentResult,
	maxComments int,
	apiKey string,
) *models.ReviewResult {

	if maxComments <= 0 {
		maxComments = agents.DefaultMaxComments
	}

	// Build the consolidation prompt
	prompt := buildConsolidationPrompt(agentResults)
	systemPrompt := fmt.Sprintf(agents.ConsolidatorSystemPrompt, maxComments)

	fmt.Printf("🔄 [Consolidator] Sending %d agent results to LLM for intelligent consolidation\n",
		len(agentResults))
	fmt.Printf("   📏 [Consolidator] Prompt size: %d chars (~%d tokens)\n",
		len(prompt), len(prompt)/4)

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{"text": prompt},
				},
			},
		},
		"system_instruction": map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": systemPrompt},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":      0.0,
			"maxOutputTokens":  8192,
			"responseMimeType": "application/json",
			"responseSchema":   responseSchema,
		},
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		fmt.Printf("   ⚠️ [Consolidator] Marshal error: %v — falling back to deterministic\n", err)
		return agents.DeterministicConsolidate(agentResults, maxComments)
	}

	url := geminiFlashURL + "?key=" + apiKey
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		fmt.Printf("   ⚠️ [Consolidator] Request error: %v — falling back to deterministic\n", err)
		return agents.DeterministicConsolidate(agentResults, maxComments)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("   ⚠️ [Consolidator] API error: %v — falling back to deterministic\n", err)
		return agents.DeterministicConsolidate(agentResults, maxComments)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("   ⚠️ [Consolidator] Read error: %v — falling back to deterministic\n", err)
		return agents.DeterministicConsolidate(agentResults, maxComments)
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("   ⚠️ [Consolidator] API returned %d: %s — falling back to deterministic\n",
			resp.StatusCode, string(respBody))
		return agents.DeterministicConsolidate(agentResults, maxComments)
	}

	// Parse the Gemini response
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}

	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		fmt.Printf("   ⚠️ [Consolidator] Parse error: %v — falling back to deterministic\n", err)
		return agents.DeterministicConsolidate(agentResults, maxComments)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		fmt.Printf("   ⚠️ [Consolidator] Empty response — falling back to deterministic\n")
		return agents.DeterministicConsolidate(agentResults, maxComments)
	}

	responseText := geminiResp.Candidates[0].Content.Parts[0].Text

	// Parse the JSON response
	var result models.ReviewResult
	cleaned := strings.TrimSpace(responseText)
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
		geminiResp.UsageMetadata.PromptTokenCount,
		geminiResp.UsageMetadata.CandidatesTokenCount)

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
