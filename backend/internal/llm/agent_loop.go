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
	"regexp"
	"strings"
	"time"
)

const maxToolIterations = 8

// responseSchema is the JSON schema enforced on Gemini's output.
// Using responseMimeType + responseSchema guarantees valid JSON.
var responseSchema = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"summary": map[string]interface{}{
			"type":        "string",
			"description": "Brief overview of issues found, or 'No issues found' if clean",
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
						"description": "Problem description followed by fix suggestion. Max 2 sentences.",
					},
				},
				"required": []string{"file", "line", "severity", "layer", "message"},
			},
		},
	},
	"required": []string{"summary", "comments"},
}

// RunAgentReview executes a single specialist agent with the agentic loop.
// It sends the initial prompt, handles tool calls, and returns the agent's findings.
func RunAgentReview(
	ctx context.Context,
	client *http.Client,
	config agents.AgentConfig,
	toolExecutor *ToolExecutor,
) agents.AgentResult {

	result := agents.AgentResult{Agent: config.Type}

	// Build the initial prompt with diff-anchored context
	prompt := buildAgentPrompt(config)
	apiKey := config.APIKey

	fmt.Printf("🤖 [%s] Starting review (%d files, %d deps)\n",
		config.Type, len(config.ChangedFiles), len(config.Dependencies))
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
		genConfig := map[string]interface{}{
			"temperature":     0.2,
			"maxOutputTokens": 65536,
		}

		reqBody := map[string]interface{}{
			"contents": messages,
			"system_instruction": map[string]interface{}{
				"parts": []map[string]interface{}{
					{"text": config.SystemPrompt},
				},
			},
			"generationConfig": genConfig,
		}

		// Add tool declarations if we have a tool executor
		if toolExecutor != nil {
			reqBody["tools"] = []map[string]interface{}{
				{
					"function_declarations": AgentToolDeclarations(),
				},
			}
			// Allow the model to decide between text and tool calls
			reqBody["tool_config"] = map[string]interface{}{
				"function_calling_config": map[string]interface{}{
					"mode": "AUTO",
				},
			}
			// NOTE: Gemini does NOT support responseMimeType with function calling.
			// JSON output is enforced via the hardened system prompt instead.
		} else {
			// No tools — we can enforce JSON schema on the response
			genConfig["responseMimeType"] = "application/json"
			genConfig["responseSchema"] = responseSchema
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
			// Retry once on empty candidates (Gemini safety filter)
			if iteration == 0 {
				fmt.Printf("  ⚠️ [%s] No candidates — retrying with nudge\n", config.Type)
				messages = append(messages, map[string]interface{}{
					"role": "user",
					"parts": []map[string]interface{}{
						{"text": "Please analyze the code changes and respond with a JSON object containing your findings. If you find no issues, respond with {\"summary\": \"No issues found\", \"comments\": []}."},
					},
				})
				continue
			}
			result.Summary = fmt.Sprintf("No findings from %s agent", config.Type)
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
		if len(textParts) > 0 && hasFunctionCall {
			fmt.Printf("  📝 [%s] Got partial text + function call, continuing loop\n", config.Type)
		}

		// Empty response — retry once with a nudge
		if !hasFunctionCall && len(textParts) == 0 {
			if iteration == 0 {
				fmt.Printf("  ⚠️ [%s] Empty response — retrying with nudge\n", config.Type)
				messages = append(messages, map[string]interface{}{
					"role": "user",
					"parts": []map[string]interface{}{
						{"text": "Your previous response was empty. Please analyze the code changes shown above and respond with a JSON object. Focus on the diff hunks marked with '+'. If no issues found, respond with {\"summary\": \"No issues found\", \"comments\": []}."},
					},
				})
				continue
			}
			fmt.Printf("  ⚠️ [%s] Empty response after retry at iteration %d\n", config.Type, iteration+1)
			result.Summary = fmt.Sprintf("No findings from %s agent", config.Type)
			return result
		}
	}

	result.Error = fmt.Errorf("exceeded max tool iterations (%d)", maxToolIterations)
	return result
}

// buildAgentPrompt constructs the user prompt for a specialist agent.
// Key improvement: diff hunks are shown INLINE with each file so agents
// know exactly which lines changed (marked with '+').
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
		b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString("PR CONTEXT (Developer Intent)\n")
		b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
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

	// Extract per-file diff hunks
	diffMap := extractChangedLinesFromDiff(config.Diff)

	// Changed files with full content, line numbers, AND inline diff hunks
	b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
	b.WriteString("PRIMARY ANALYSIS TARGET: Changed Code Files\n")
	b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
	b.WriteString("INSTRUCTION: Analyze ONLY the lines marked with '+' in the DIFF HUNKS below.\n")
	b.WriteString("Use the full file content for context, but flag issues ONLY in changed lines.\n\n")

	for _, path := range getFileKeys(config.ChangedFiles) {
		content := config.ChangedFiles[path]
		b.WriteString(fmt.Sprintf("\n═══ FILE: %s ═══\n", path))

		// Full file content with line numbers (for context)
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			b.WriteString(fmt.Sprintf("%d: %s\n", i+1, line))
		}

		// Inline diff hunks for this file (shows what actually changed)
		if diffSections, hasDiff := diffMap[path]; hasDiff && len(diffSections) > 0 {
			b.WriteString("\n────────────────────────────────────────\n")
			b.WriteString(fmt.Sprintf("DIFF HUNKS FOR: %s (ANALYZE THESE CHANGES)\n", path))
			b.WriteString("────────────────────────────────────────\n")
			for i, section := range diffSections {
				b.WriteString(fmt.Sprintf("/* Change Block %d:\n%s\n*/\n\n", i+1, section))
			}
		}
	}

	// Dependencies (from code graph)
	if len(config.Dependencies) > 0 {
		b.WriteString("\n═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString("SUPPORTING CONTEXT: Code Graph Dependencies\n")
		b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString("These files are NOT being reviewed. They provide context for understanding\n")
		b.WriteString("the changed code's dependencies, types, and function signatures.\n\n")
		for _, path := range getFileKeys(config.Dependencies) {
			b.WriteString(fmt.Sprintf("--- %s ---\n%s\n\n", path, config.Dependencies[path]))
		}
	}

	// Repo structure (mainly for Structure agent)
	if config.RepoStructure != "" {
		b.WriteString("\n═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString("SUPPORTING CONTEXT: Repository Structure\n")
		b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString(config.RepoStructure)
		b.WriteString("\n")
	}

	b.WriteString("\n═══════════════════════════════════════════════════════════════════════════════\n")
	b.WriteString("BEGIN ANALYSIS NOW.\n")
	b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
	b.WriteString("REMINDER: Your response MUST be a valid JSON object with \"summary\" and \"comments\" keys.\n")
	b.WriteString("Do NOT output plain text, code comments, or markdown. Output ONLY JSON.\n")

	return b.String()
}

// parseAgentResponse parses the JSON response from an agent into ReviewResult.
// Falls back to text extraction if JSON parsing fails.
func parseAgentResponse(text string) models.ReviewResult {
	// Clean up the response — strip markdown fences if present
	cleaned := strings.TrimSpace(text)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var result models.ReviewResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		// Try fallback: maybe it's a naked array of comments
		var comments []models.ReviewComment
		if errArray := json.Unmarshal([]byte(cleaned), &comments); errArray == nil {
			result.Comments = comments
			result.Summary = "Review comments"
			return result
		}

		// Fallback: extract findings from plain text formats
		// Gemini sometimes outputs grep-like or code-comment formats
		extracted := parseTextFallback(cleaned)
		if len(extracted) > 0 {
			fmt.Printf("  🔄 JSON parse failed, extracted %d comments from text fallback\n", len(extracted))
			return models.ReviewResult{
				Summary:  fmt.Sprintf("Extracted %d findings from text response", len(extracted)),
				Comments: extracted,
			}
		}

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

// grepLinePattern matches: file/path.go:42: severity: message
var grepLinePattern = regexp.MustCompile(`^([^\s:]+\.\w+):(\d+):\s*(critical|warning|info):\s*(.+)`)

// commentLinePattern matches: // Line 42
var commentLinePattern = regexp.MustCompile(`(?i)//\s*Line\s+(\d+)`)

// commentSeverityPattern matches: // critical: message or // warning: message
var commentSeverityPattern = regexp.MustCompile(`(?i)//\s*(critical|warning|info):\s*(.+)`)

// commentFilePattern matches: // path/to/file.go
var commentFilePattern = regexp.MustCompile(`^//\s*([^\s]+\.\w+)\s*$`)

// parseTextFallback extracts review comments from non-JSON text responses.
// Handles two formats seen in production:
//  1. Grep-like: "file.go:42: warning: Message here"
//  2. Code-comment: "// file.go\n// Line 42\n// critical: Message"
func parseTextFallback(text string) []models.ReviewComment {
	var comments []models.ReviewComment

	lines := strings.Split(text, "\n")

	// Try grep-like format first: file:line: severity: message
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if matches := grepLinePattern.FindStringSubmatch(line); len(matches) == 5 {
			lineNum := 0
			fmt.Sscanf(matches[2], "%d", &lineNum)
			severity := strings.ToLower(matches[3])
			layer := inferLayer(severity, matches[4])
			comments = append(comments, models.ReviewComment{
				File:     matches[1],
				Line:     lineNum,
				Severity: severity,
				Layer:    layer,
				Message:  strings.TrimSpace(matches[4]),
			})
		}
	}

	if len(comments) > 0 {
		return comments
	}

	// Try code-comment format: // file\n// Line N\n// severity: message
	var currentFile string
	var currentLine int

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Match file path: // path/to/file.go
		if matches := commentFilePattern.FindStringSubmatch(line); len(matches) == 2 {
			currentFile = matches[1]
			currentLine = 0
			continue
		}

		// Match line number: // Line 42
		if matches := commentLinePattern.FindStringSubmatch(line); len(matches) == 2 {
			fmt.Sscanf(matches[1], "%d", &currentLine)
			continue
		}

		// Match severity + message: // critical: Path traversal...
		if matches := commentSeverityPattern.FindStringSubmatch(line); len(matches) == 3 && currentFile != "" {
			severity := strings.ToLower(matches[1])
			layer := inferLayer(severity, matches[2])
			comments = append(comments, models.ReviewComment{
				File:     currentFile,
				Line:     currentLine,
				Severity: severity,
				Layer:    layer,
				Message:  strings.TrimSpace(matches[2]),
			})
		}
	}

	return comments
}

// inferLayer guesses the review layer from severity and message content.
func inferLayer(severity, message string) string {
	lower := strings.ToLower(message)

	if strings.Contains(lower, "injection") || strings.Contains(lower, "traversal") ||
		strings.Contains(lower, "xss") || strings.Contains(lower, "auth") ||
		strings.Contains(lower, "secret") || strings.Contains(lower, "credential") ||
		strings.Contains(lower, "ssrf") || strings.Contains(lower, "csrf") {
		return "security"
	}

	if strings.Contains(lower, "dead code") || strings.Contains(lower, "organization") ||
		strings.Contains(lower, "architecture") || strings.Contains(lower, "circular") ||
		strings.Contains(lower, "pattern") || strings.Contains(lower, "package") {
		return "architecture"
	}

	if strings.Contains(lower, "performance") || strings.Contains(lower, "allocation") ||
		strings.Contains(lower, "o(n") || strings.Contains(lower, "cache") {
		return "performance"
	}

	if severity == "critical" && (strings.Contains(lower, "security") || strings.Contains(lower, "vuln")) {
		return "security"
	}

	return "bug"
}
