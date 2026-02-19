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

const maxToolIterations = 5

// RunAgentReview executes a single specialist agent with the agentic loop.
// It sends the initial prompt, handles tool calls, and returns the agent's findings.
func RunAgentReview(
	ctx context.Context,
	client *http.Client,
	config agents.AgentConfig,
	toolExecutor *ToolExecutor,
) agents.AgentResult {

	result := agents.AgentResult{Agent: config.Type}

	// Build the initial prompt
	prompt := buildAgentPrompt(config)
	apiKey := config.APIKey

	fmt.Printf("🤖 [%s] Starting review (%d files, %d deps)\n",
		config.Type, len(config.ChangedFiles), len(config.Dependencies))

	// Log prompt size for debugging
	fmt.Printf("  📏 [%s] Prompt size: %d chars (~%d tokens)\n",
		config.Type, len(prompt), len(prompt)/4)

	// Build the request with tool declarations
	messages := []map[string]interface{}{
		{
			"role": "user",
			"parts": []map[string]interface{}{
				{"text": prompt},
			},
		},
	}

	// Agentic loop: send → maybe tool call → send result → repeat
	for iteration := 0; iteration <= maxToolIterations; iteration++ {
		reqBody := map[string]interface{}{
			"contents": messages,
			"system_instruction": map[string]interface{}{
				"parts": []map[string]interface{}{
					{"text": config.SystemPrompt},
				},
			},
			"generationConfig": map[string]interface{}{
				"temperature":     0.2,
				"maxOutputTokens": 65536,
			},
		}

		// Add tool declarations if we have a tool executor
		if toolExecutor != nil {
			reqBody["tools"] = []map[string]interface{}{
				{
					"function_declarations": AgentToolDeclarations(),
				},
			}
		}

		bodyJSON, err := json.Marshal(reqBody)
		if err != nil {
			result.Error = fmt.Errorf("marshal request: %w", err)
			return result
		}

		url := geminiURL + "?key=" + apiKey
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
		if err != nil {
			result.Error = fmt.Errorf("create request: %w", err)
			return result
		}
		req.Header.Set("Content-Type", "application/json")

		start := time.Now()
		resp, err := client.Do(req)
		elapsed := time.Since(start)
		if err != nil {
			result.Error = fmt.Errorf("API call: %w", err)
			return result
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			result.Error = fmt.Errorf("read response: %w", err)
			return result
		}

		if resp.StatusCode != http.StatusOK {
			result.Error = fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
			return result
		}

		// Parse the Gemini response
		var geminiResp struct {
			Candidates []struct {
				Content struct {
					Parts []json.RawMessage `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
			} `json:"candidates"`
			UsageMetadata struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
			} `json:"usageMetadata"`
		}

		if err := json.Unmarshal(respBody, &geminiResp); err != nil {
			result.Error = fmt.Errorf("parse response: %w", err)
			return result
		}

		result.InputTokens += geminiResp.UsageMetadata.PromptTokenCount
		result.OutputTokens += geminiResp.UsageMetadata.CandidatesTokenCount

		if len(geminiResp.Candidates) == 0 {
			result.Error = fmt.Errorf("no candidates in response")
			return result
		}

		candidate := geminiResp.Candidates[0]

		// Check finish reason for truncation
		if candidate.FinishReason == "MAX_TOKENS" {
			fmt.Printf("  ⚠️ [%s] Response truncated (MAX_TOKENS) at iteration %d\n", config.Type, iteration+1)
		}

		// Parse parts — each part can be either text or functionCall
		var hasFunctionCall bool
		var textParts []string

		for _, rawPart := range candidate.Content.Parts {
			var part struct {
				Text         string `json:"text"`
				FunctionCall *struct {
					Name string                 `json:"name"`
					Args map[string]interface{} `json:"args"`
				} `json:"functionCall"`
			}
			if err := json.Unmarshal(rawPart, &part); err != nil {
				continue
			}

			if part.FunctionCall != nil {
				hasFunctionCall = true
				toolCall := agents.ToolCallRequest{
					Name: part.FunctionCall.Name,
					Args: part.FunctionCall.Args,
				}

				fmt.Printf("  🔧 [%s] Tool call: %s(%v) [iter %d, %.1fs]\n",
					config.Type, toolCall.Name, toolCall.Args, iteration+1, elapsed.Seconds())

				// Execute the tool
				toolResult := toolExecutor.Execute(ctx, toolCall)
				result.ToolCalls++

				// Log the tool response for transparency
				fmt.Printf("  📨 [%s] Tool response (%s): %d chars\n",
					config.Type, toolCall.Name, len(toolResult.Content))
				fmt.Println("  ────────────────────────────────────────")
				// Print first 2000 chars of tool response
				responsePreview := toolResult.Content
				if len(responsePreview) > 2000 {
					responsePreview = responsePreview[:2000] + "\n  ...(truncated for display)"
				}
				fmt.Println(responsePreview)
				fmt.Println("  ────────────────────────────────────────")

				// Append the assistant's function call and the tool result to messages
				messages = append(messages,
					map[string]interface{}{
						"role": "model",
						"parts": []map[string]interface{}{
							{
								"functionCall": map[string]interface{}{
									"name": toolCall.Name,
									"args": toolCall.Args,
								},
							},
						},
					},
					map[string]interface{}{
						"role": "user",
						"parts": []map[string]interface{}{
							{
								"functionResponse": map[string]interface{}{
									"name": toolCall.Name,
									"response": map[string]interface{}{
										"content": toolResult.Content,
									},
								},
							},
						},
					},
				)
			} else if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
		}

		// If we have text parts (final response), parse them
		if len(textParts) > 0 && !hasFunctionCall {
			fullText := strings.Join(textParts, "\n")
			parsed := parseAgentResponse(fullText)
			result.Comments = parsed.Comments
			result.Summary = parsed.Summary
			fmt.Printf("  ✅ [%s] Done: %d comments (%.1fs, %d tool calls)\n",
				config.Type, len(result.Comments), elapsed.Seconds(), result.ToolCalls)
			return result
		}

		// If we got text AND function calls, store text for later
		// (Gemini sometimes returns partial text + function call)
		if len(textParts) > 0 && hasFunctionCall {
			fmt.Printf("  📝 [%s] Got partial text + function call, continuing loop\n", config.Type)
		}

		// If we only got function calls, loop continues
		if !hasFunctionCall && len(textParts) == 0 {
			// No function call and no text — check if this is a safety/empty response
			fmt.Printf("  ⚠️ [%s] Empty response (no text, no function call) at iteration %d\n",
				config.Type, iteration+1)
			result.Summary = fmt.Sprintf("No findings from %s agent (empty response)", config.Type)
			return result
		}
	}

	result.Error = fmt.Errorf("exceeded max tool iterations (%d)", maxToolIterations)
	return result
}

// buildAgentPrompt constructs the user prompt for a specialist agent.
func buildAgentPrompt(config agents.AgentConfig) string {
	var b strings.Builder

	// Chunk header
	if config.ChunkTotal > 1 {
		b.WriteString(fmt.Sprintf("=== CHUNK %d/%d ===\n", config.ChunkIndex, config.ChunkTotal))
		if len(config.CrossRefs) > 0 {
			b.WriteString(fmt.Sprintf("Files in other chunks that relate to this one: %s\n", strings.Join(config.CrossRefs, ", ")))
		}
		b.WriteString("\n")
	}

	// PR Context
	if config.PRContext.Title != "" {
		b.WriteString("== PR CONTEXT ==\n")
		b.WriteString(fmt.Sprintf("Title: %s\n", config.PRContext.Title))
		if config.PRContext.Body != "" {
			body := config.PRContext.Body
			if len(body) > 2000 {
				body = body[:2000] + "..."
			}
			b.WriteString(fmt.Sprintf("Description: %s\n", body))
		}
		b.WriteString("\n")
	}

	// Changed files with full content and line numbers
	b.WriteString("== CHANGED FILES (Full Content) ==\n")
	for _, path := range getFileKeys(config.ChangedFiles) {
		content := config.ChangedFiles[path]
		b.WriteString(fmt.Sprintf("\n═══ FILE: %s ═══\n", path))
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			b.WriteString(fmt.Sprintf("%d: %s\n", i+1, line))
		}
	}

	// Diff hunks
	if config.Diff != "" {
		b.WriteString("\n== RECENT CHANGES (Diff) ==\n")
		b.WriteString(config.Diff)
		b.WriteString("\n")
	}

	// Dependencies (from code graph)
	if len(config.Dependencies) > 0 {
		b.WriteString("\n== CODE GRAPH DEPENDENCIES ==\n")
		for _, path := range getFileKeys(config.Dependencies) {
			b.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", path, config.Dependencies[path]))
		}
	}

	// Repo structure (mainly for Structure agent)
	if config.RepoStructure != "" {
		b.WriteString("\n== REPOSITORY STRUCTURE ==\n")
		b.WriteString(config.RepoStructure)
		b.WriteString("\n")
	}

	return b.String()
}

// parseAgentResponse parses the JSON response from an agent into ReviewResult.
func parseAgentResponse(text string) models.ReviewResult {
	// Clean up the response — strip markdown fences if present
	cleaned := strings.TrimSpace(text)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var result models.ReviewResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		// Log the raw response for debugging (no truncation warning)
		preview := cleaned
		if len(preview) > 1000 {
			preview = preview[:1000] + "\n...[RESPONSE TRUNCATED FOR DISPLAY]..."
		}
		return models.ReviewResult{
			Summary: fmt.Sprintf("Agent response parse error: %v\nRaw: %s", err, preview),
		}
	}
	return result
}
