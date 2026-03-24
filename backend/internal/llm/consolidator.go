package llm

import (
	"code-review/backend/internal/agents"
	"code-review/backend/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// consolidatorResponseSchema enforces structured JSON output with a mandatory
// "thinking" field for chain-of-thought verification reasoning.
var consolidatorResponseSchema = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"thinking": map[string]interface{}{
			"type":        "string",
			"description": "Trace findings, verify them against the diff, and explain deduplication/dropping decisions before writing the final output.",
		},
		"summary": map[string]interface{}{
			"type":        "string",
			"description": "1-2 sentence high-level summary of the consolidated review",
		},
		"comments": map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file": map[string]interface{}{
						"type":        "string",
						"description": "Relative file path from repo root",
					},
					"line": map[string]interface{}{
						"type":        "integer",
						"description": "Line number from the '+' lines in the diff",
					},
					"severity": map[string]interface{}{
						"type": "string",
						"enum": []string{"critical", "warning", "info"},
					},
					"layer": map[string]interface{}{
						"type": "string",
						"enum": []string{"bug", "performance", "security", "architecture", "lint"},
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Concise explanation. Max 3-4 sentences.",
					},
				},
				"required": []string{"file", "line", "severity", "layer", "message"},
			},
		},
		"resolutions": map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"comment_id": map[string]interface{}{
						"type":        "integer",
						"description": "The ID of the previous PR comment being resolved",
					},
					"status": map[string]interface{}{
						"type": "string",
						"enum": []string{"resolved", "unresolved"},
					},
					"reason": map[string]interface{}{
						"type":        "string",
						"description": "Why the finding was resolved or remains unresolved",
					},
				},
				"required": []string{"comment_id", "status", "reason"},
			},
		},
	},
	"required": []string{"thinking", "summary", "comments", "resolutions"},
}

// RunConsolidation sends all specialist agent results to the LLM for intelligent
// consolidation. The LLM can detect semantically similar issues with different
// wording, assess comment quality, and resolve conflicts between agents.
//
// Falls back to DeterministicConsolidate on any LLM failure.
func RunConsolidation(
	ctx context.Context,
	provider LLMProvider,
	agentResults []agents.AgentResult,
	diff string,
	maxComments int,
	matchSummary string,
	toolExecutor *ToolExecutor,
) *models.ReviewResult {

	if maxComments <= 0 {
		maxComments = agents.DefaultMaxComments
	}

	// Build the consolidation prompt
	prompt := buildConsolidationPrompt(agentResults, diff)
	systemPrompt := agents.BuildConsolidatorSystemPrompt(maxComments, matchSummary)

	fmt.Printf("🔄 [Consolidator] Sending %d agent results to LLM for intelligent consolidation\n",
		len(agentResults))
	fmt.Printf("   📏 [Consolidator] Prompt size: %d chars (~%d tokens)\n",
		len(prompt), len(prompt)/4)

	var tools []ToolDeclaration
	if toolExecutor != nil {
		tools = AgentToolDeclarations()
	}

	messages := []Message{
		{
			Role: "user",
			Parts: []Part{
				{Text: prompt},
			},
		},
	}

	start := time.Now()
	var finalResult *models.ReviewResult
	var lastErr error
	var totalInputTokens, totalOutputTokens int

	for iteration := 0; iteration <= maxToolIterations; iteration++ {
		req := GenerateRequest{
			SystemPrompt:   systemPrompt,
			Messages:       messages,
			Tools:          tools,
			Temperature:    0.0,
			ResponseJSON:   toolExecutor == nil, // enforce JSON if no tools
			ResponseSchema: consolidatorResponseSchema,
		}

		resp, err := provider.GenerateContent(ctx, req)
		if err != nil {
			lastErr = err
			break
		}

		totalInputTokens += resp.InputTokens
		totalOutputTokens += resp.OutputTokens

		if resp.FinishReason == "EMPTY" || (resp.Text == "" && len(resp.FunctionCalls) == 0) {
			lastErr = fmt.Errorf("empty response")
			break
		}

		if len(resp.FunctionCalls) > 0 {
			modelParts := resp.ModelParts
			if len(modelParts) == 0 {
				for _, fc := range resp.FunctionCalls {
					modelParts = append(modelParts, Part{FunctionCall: fc})
				}
			}
			messages = append(messages, Message{
				Role:  "model",
				Parts: modelParts,
			})

			fmt.Printf("  🔧 [Consolidator] Executing %d tool calls [iter %d]\n", len(resp.FunctionCalls), iteration+1)

			var wg sync.WaitGroup
			funcParts := make([]Part, len(resp.FunctionCalls))

			for i, fc := range resp.FunctionCalls {
				wg.Add(1)
				go func(idx int, call *FunctionCall) {
					defer wg.Done()
					req := agents.ToolCallRequest{
						Name: call.Name,
						Args: call.Args,
					}
					fmt.Printf("     ├── Call %d: %s(%v)\n", idx+1, call.Name, call.Args)
					var content string
					if toolExecutor != nil {
						res := toolExecutor.Execute(ctx, req)
						content = res.Content
					} else {
						content = "Error: Tool execution is not available in this context."
					}
					fmt.Printf("     └── Resp %d: %s (%d chars)\n", idx+1, call.Name, len(content))

					funcParts[idx] = Part{
						FunctionResp: &FunctionResponse{
							ID:      call.ID,
							Name:    call.Name,
							Content: content,
						},
					}
				}(i, fc)
			}
			wg.Wait()

			messages = append(messages, Message{
				Role:  "function",
				Parts: funcParts,
			})
		} else if resp.Text != "" {
			// Parse the JSON response
			var result models.ReviewResult
			cleaned := strings.TrimSpace(resp.Text)
			cleaned = strings.TrimPrefix(cleaned, "```json")
			cleaned = strings.TrimPrefix(cleaned, "```")
			cleaned = strings.TrimSuffix(cleaned, "```")
			cleaned = strings.TrimSpace(cleaned)

			if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
				lastErr = err
				fmt.Printf("   ⚠️ [Consolidator] JSON unmarshal error: %v\n", err)
				fmt.Printf("   Raw response: %.500s\n", cleaned)
			} else {
				finalResult = &result
			}
			break
		}
	}

	elapsed := time.Since(start)

	if finalResult == nil {
		fmt.Printf("   ⚠️ [Consolidator] Loop failed: %v — falling back to deterministic\n", lastErr)
		return agents.DeterministicConsolidate(agentResults, maxComments)
	}

	fmt.Printf("   ✅ [Consolidator] LLM consolidation complete: %d comments (%.1fs, %d→%d tokens)\n",
		len(finalResult.Comments), elapsed.Seconds(),
		totalInputTokens, totalOutputTokens)

	return finalResult
}

// buildConsolidationPrompt formats all agent results into a structured prompt
// for the consolidator LLM.
func buildConsolidationPrompt(results []agents.AgentResult, diff string) string {
	var b strings.Builder

	b.WriteString("Below are the findings from 3 specialist code review agents.\n")
	b.WriteString("Consolidate them according to your instructions.\n\n")
	b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
	b.WriteString("TARGET DIFF (Verify Line Numbers against NEW_LIVE_CODE):\n")
	b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")

	// Convert raw diff to safe diff to show [NEW_LIVE_CODE] clearly
	diffMap := extractChangedLinesFromDiff(diff)
	for path, sections := range diffMap {
		b.WriteString(fmt.Sprintf("\n--- DIFF FOR: %s ---\n", path))
		for _, sec := range sections {
			b.WriteString(formatSafeDiff(sec))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n═══════════════════════════════════════════════════════════════════════════════\n\n")

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

		// Resolutions MUST be serialized before the early continue below,
		// otherwise agents with 0 comments but resolved findings get skipped.
		if len(r.Resolutions) > 0 {
			b.WriteString(fmt.Sprintf("\nResolutions (%d):\n", len(r.Resolutions)))
			for _, res := range r.Resolutions {
				b.WriteString(fmt.Sprintf("- CommentID: %d | Status: %s | Reason: %s\n", res.CommentID, res.Status, res.Reason))
			}
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
