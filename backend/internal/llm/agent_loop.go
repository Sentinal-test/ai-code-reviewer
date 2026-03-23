package llm

import (
	"code-review/backend/internal/agents"
	"code-review/backend/internal/models"
	"code-review/backend/internal/rules"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

const maxToolIterations = 10

// responseSchema is the JSON schema enforced on Gemini's output.
// Using responseMimeType + responseSchema guarantees valid JSON.
var responseSchema = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"thinking": map[string]interface{}{
			"type":        "string",
			"description": "Trace data flows, evaluate developer intent, and verify tool outputs BEFORE writing comments.",
		},
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
	"required": []string{"thinking", "summary", "comments"},
}

// RunAgentReview executes a single specialist agent with the agentic loop.
// It sends the initial prompt, handles tool calls, and returns the agent's findings.
func RunAgentReview(
	ctx context.Context,
	provider LLMProvider,
	config agents.AgentConfig,
	toolExecutor *ToolExecutor,
) agents.AgentResult {

	result := agents.AgentResult{Agent: config.Type}

	// 1. Build the heavy, static context (to be cached)
	staticContext := buildStaticContext(config)

	fmt.Printf("🤖 [%s] Starting review (%d files, %d deps)\n",
		config.Type, len(config.ChangedFiles), len(config.Dependencies))

	// Create the cache for this agent's specific context
	cacheName := config.CacheID
	var err error

	var tools []ToolDeclaration
	if toolExecutor != nil {
		tools = AgentToolDeclarations()
	}

	// Only bother caching if the context is substantial enough (> 135,000 chars is roughly 33,000 tokens)
	// Gemini API requires a minimum of 32,768 tokens for Context Caching.
	if cacheName == "" && len(staticContext) > 135000 {
		fmt.Printf("  📦 [%s] Creating Context Cache (~%d tokens)...\n", config.Type, len(staticContext)/4)
		cacheStart := time.Now()

		cacheName, err = provider.CreateCache(ctx, config.SystemPrompt, staticContext, tools)
		if err != nil {
			fmt.Printf("  ⚠️ [%s] Failed to create cache, falling back to inline context: %v\n", config.Type, err)
		} else if cacheName != "" {
			fmt.Printf("  ✅ [%s] Cache created in %.1fs: %s\n", config.Type, time.Since(cacheStart).Seconds(), cacheName)
			defer provider.DeleteCache(context.Background(), cacheName)
		} else {
			fmt.Printf("  ℹ️ [%s] Caching skipped (not supported/required by provider)\n", config.Type)
		}
	}

	// 2. Build the lightweight, dynamic prompt
	dynamicPrompt := buildDynamicPrompt(config)

	// If caching failed or skipped, we must inline the static context into the dynamic prompt
	if cacheName == "" {
		dynamicPrompt = staticContext + "\n\n" + dynamicPrompt
	}

	fmt.Printf("  📏 [%s] Prompt size: %d chars (~%d tokens)\n",
		config.Type, len(dynamicPrompt), len(dynamicPrompt)/4)

	// LOG: Complete Raw Input
	fmt.Printf("\n--- [%s] RAW SYSTEM PROMPT ---\n%s\n", config.Type, config.SystemPrompt)
	fmt.Printf("\n--- [%s] RAW USER PROMPT (Dynamic + Static) ---\n%s\n", config.Type, dynamicPrompt)
	fmt.Println("--------------------------------------------------------------------------------")

	// Build the initial request messages
	messages := []Message{
		{
			Role: "user",
			Parts: []Part{
				{Text: dynamicPrompt},
			},
		},
	}

	// Agentic loop: send → maybe tool call → send result → repeat
	for iteration := 0; iteration <= maxToolIterations; iteration++ {
		req := GenerateRequest{
			SystemPrompt:  config.SystemPrompt,
			Messages:      messages,
			Tools:         tools,
			CachedContent: cacheName,
			Temperature:   0.0,
			ResponseJSON:  toolExecutor == nil, // enforce JSON if no tools
		}

		start := time.Now()
		resp, err := provider.GenerateContent(ctx, req)
		elapsed := time.Since(start)

		if err != nil {
			result.Error = fmt.Errorf("LLM API error: %w", err)
			return result
		}

		result.InputTokens += resp.InputTokens
		result.OutputTokens += resp.OutputTokens

		if resp.FinishReason == "EMPTY" {
			// Retry once on empty candidates (safety filter or model error)
			if iteration == 0 {
				fmt.Printf("  ⚠️ [%s] No candidates — retrying with nudge\n", config.Type)
				lastMsg := &messages[len(messages)-1]
				lastMsg.Parts = append(lastMsg.Parts, Part{
					Text: "\n\nPlease analyze the code changes and respond with a JSON object containing your findings. If you find no issues, respond with {\"summary\": \"No issues found\", \"comments\": []}.",
				})
				continue
			}
			result.Summary = fmt.Sprintf("No findings from %s agent", config.Type)
			return result
		}

		if resp.FinishReason == "MAX_TOKENS" {
			fmt.Printf("  ⚠️ [%s] Response truncated (MAX_TOKENS) at iteration %d\n", config.Type, iteration+1)
		}

		// If we have tool calls, execute them and build the conversation history
		if len(resp.FunctionCalls) > 0 {
			// Append model turn — use ModelParts if available (Gemini includes
			// thought parts that must be replayed), otherwise fall back to all tools.
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

			fmt.Printf("  🔧 [%s] Executing %d parallel tool calls [iter %d, %.1fs]\n",
				config.Type, len(resp.FunctionCalls), iteration+1, elapsed.Seconds())

			var wg sync.WaitGroup
			funcParts := make([]Part, len(resp.FunctionCalls))

			for i, fc := range resp.FunctionCalls {
				wg.Add(1)
				result.ToolCalls++

				go func(idx int, call *FunctionCall) {
					defer wg.Done()
					req := agents.ToolCallRequest{
						Name: call.Name,
						Args: call.Args,
					}

					fmt.Printf("     ├── Call %d: %s(%v)\n", idx+1, call.Name, call.Args)
					res := toolExecutor.Execute(ctx, req)
					fmt.Printf("     └── Resp %d: %s (%d chars)\n", idx+1, call.Name, len(res.Content))

					funcParts[idx] = Part{
						FunctionResp: &FunctionResponse{
							ID:      call.ID,
							Name:    call.Name,
							Content: res.Content,
						},
					}
				}(i, fc)
			}
			wg.Wait()

			// Append function response turn with all tool results safely ordered
			messages = append(messages, Message{
				Role:  "function",
				Parts: funcParts,
			})
		} else if resp.Text != "" {
			// Text-only response (final answer) — parse it
			parsed := parseAgentResponse(resp.Text)
			result.Comments = parsed.Comments
			result.Summary = parsed.Summary
			fmt.Printf("  ✅ [%s] Done: %d comments (%.1fs, %d tool calls)\n",
				config.Type, len(result.Comments), elapsed.Seconds(), result.ToolCalls)
			return result
		} else {
			// Empty text response — retry once with a nudge
			if iteration == 0 {
				fmt.Printf("  ⚠️ [%s] Empty response — retrying with nudge\n", config.Type)
				lastMsg := &messages[len(messages)-1]
				lastMsg.Parts = append(lastMsg.Parts, Part{
					Text: "\n\nYour previous response was empty. Please analyze the code changes shown above and respond with a JSON object. Focus on the diff hunks marked with '+'. If no issues found, respond with {\"summary\": \"No issues found\", \"comments\": []}.",
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

// buildStaticContext constructs the heavy, cacheable portion of the prompt:
// 1. Changed files (with diffs)
// 2. Dependencies
// 3. Repo structure
// buildStaticContext constructs the heavy, cacheable portion of the prompt:
// Global Context: Repo structure and dependencies.
func buildStaticContext(config agents.AgentConfig) string {
	var b strings.Builder

	// Dependencies (from code graph) - Background Noise
	if len(config.Dependencies) > 0 {
		depStart := b.Len()
		b.WriteString("\n═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString("GLOBAL CONTEXT: Code Graph Dependencies\n")
		b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString("These files are NOT being reviewed. They provide background context for understanding\n")
		b.WriteString("the changed code's dependencies, types, and function signatures.\n\n")
		for _, path := range getFileKeys(config.Dependencies) {
			b.WriteString(fmt.Sprintf("--- %s ---\n%s\n\n", path, config.Dependencies[path]))
		}
		depEnd := b.Len()
		fmt.Printf("   📝 [%s] Code-Graph Context: %d chars (~%d tokens)\n",
			config.Type, depEnd-depStart, (depEnd-depStart)/4)
	}

	// Repo structure (mainly for Structure agent) - Background Noise
	if config.RepoStructure != "" {
		b.WriteString("\n═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString("GLOBAL CONTEXT: Repository Structure\n")
		b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString(config.RepoStructure)
		b.WriteString("\n")
	}

	return b.String()
}

// buildDynamicPrompt constructs the dynamic, targeted part of the prompt
// It includes PR Context, Developer Rules, and the Actionable Target (Changed Files).
func buildDynamicPrompt(config agents.AgentConfig) string {
	var b strings.Builder

	// PR History - Historical Context
	if len(config.PreviousFindings) > 0 {
		b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString("TASK CONTEXT: Previous Review Findings\n")
		b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString("The following issues were already reported in previous commits of this PR.\n")
		b.WriteString("Reference them to avoid redundant comments and to see if previous bugs were fixed.\n\n")
		for _, f := range config.PreviousFindings {
			// Only show first 200 chars of message as it's for context only
			msg := f.Message
			if len(msg) > 200 {
				msg = msg[:200] + "..."
			}
			b.WriteString(fmt.Sprintf("- [ID: %d] [%s] %s:%d: %s\n", f.CommentID, f.Layer, f.File, f.Line, msg))
		}
		b.WriteString("\n")
	}

	// Chunk header
	if config.ChunkTotal > 1 {
		b.WriteString(fmt.Sprintf("=== CHUNK %d/%d ===\n", config.ChunkIndex, config.ChunkTotal))
		if len(config.CrossRefs) > 0 {
			b.WriteString(fmt.Sprintf("Files in other chunks that relate to this one: %s\n", strings.Join(config.CrossRefs, ", ")))
		}
		b.WriteString("\n")
	}

	// PR Context - Rules of Engagement
	if config.PRContext.Title != "" {
		b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString("TASK CONTEXT: PR Intent\n")
		b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
		b.WriteString(fmt.Sprintf("Title: %s\n", config.PRContext.Title))
		if config.PRContext.Body != "" {
			body := config.PRContext.Body
			if len(body) > 1000 {
				body = body[:1000] + "..."
			}
			b.WriteString(fmt.Sprintf("Description: %s\n", body))
		}

		if len(config.PRContext.CommitMessages) > 0 {
			b.WriteString("\nRecent Commits:\n")
			for i, msg := range config.PRContext.CommitMessages {
				cleanMsg := strings.TrimSpace(msg)
				if len(cleanMsg) > 500 {
					cleanMsg = cleanMsg[:500] + "..."
				}
				b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, cleanMsg))
			}
		}
		b.WriteString("\n")
	}

	// Developer rules - Rules of Engagement
	if config.DeveloperRules != nil {
		rulesBlock := rules.FormatForPrompt(config.DeveloperRules)
		if rulesBlock != "" {
			b.WriteString(rulesBlock)
			b.WriteString("\n")
		}
	}

	// Actionable Target: Changed files with full content, line numbers, AND inline diff hunks
	b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
	b.WriteString("ACTIONABLE TARGET: Changed Code Files\n")
	b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
	b.WriteString("INSTRUCTION: Analyze ONLY the lines marked with '+' in the DIFF HUNKS below.\n")
	b.WriteString("Use the full file content for context, but flag issues ONLY in changed lines.\n\n")

	diffMap := extractChangedLinesFromDiff(config.Diff)

	for _, path := range getFileKeys(config.ChangedFiles) {
		content := config.ChangedFiles[path]
		b.WriteString(fmt.Sprintf("\n═══ FILE: %s ═══\n", path))

		diffSections := diffMap[path]
		if len(diffSections) > 0 {
			// Always use focused context (imports + ±40 lines around diff) to save costs
			b.WriteString("[Showing imports + ±40 lines around each change]\n")
			b.WriteString("[Use get_file_content tool to inspect other sections if needed]\n\n")
			focused := extractDiffWithContext(content, diffSections, 40)
			b.WriteString(focused)
		} else {
			// Fallback: If no diff sections matched, show the first 100 lines 
			lines := strings.Split(content, "\n")
			limit := 100
			if len(lines) < limit {
				limit = len(lines)
			}
			for i := 0; i < limit; i++ {
				b.WriteString(fmt.Sprintf("%d: %s\n", i+1, lines[i]))
			}
			if len(lines) > limit {
				b.WriteString(fmt.Sprintf("\n... (%d lines total, showing first %d) ...\n", len(lines), limit))
			}
		}

		// Inline diff hunks for this file (shows what actually changed)
		if diffSections, hasDiff := diffMap[path]; hasDiff && len(diffSections) > 0 {
			b.WriteString("\n────────────────────────────────────────\n")
			b.WriteString(fmt.Sprintf("DIFF HUNKS FOR: %s (ANALYZE THESE CHANGES)\n", path))
			b.WriteString("────────────────────────────────────────\n")
			b.WriteString("LEGEND:\n")
			b.WriteString("  [OLD_REMOVED_CODE] = This code was DELETED. It no longer exists in the codebase.\n")
			b.WriteString("  [NEW_LIVE_CODE]    = This code was ADDED. This is the current, active code.\n")
			b.WriteString("CRITICAL: Do NOT tell the developer to make a change that [NEW_LIVE_CODE] already implements!\n\n")
			for i, section := range diffSections {
				safeDiff := formatSafeDiff(section)
				b.WriteString(fmt.Sprintf("/* Change Block %d:\n%s*/\n\n", i+1, safeDiff))
			}
		}
	}

	b.WriteString("\n═══════════════════════════════════════════════════════════════════════════════\n")
	b.WriteString("BEGIN ANALYSIS NOW.\n")
	b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")
	b.WriteString("REMINDER: Your response MUST be a valid JSON object with \"thinking\", \"summary\", \"comments\", and \"resolutions\" keys.\n")
	b.WriteString("For \"resolutions\", output an array of objects {\"comment_id\": 123, \"status\": \"resolved\"|\"unresolved\", \"reason\": \"...\"} evaluating if previous findings were fixed based on the new diff.\n")
	b.WriteString("Do NOT output plain text, code comments, or markdown. Output ONLY JSON.\n")

	return b.String()
}

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

// BuildGlobalStaticContext creates the global context string for caching
func BuildGlobalStaticContext(config agents.AgentConfig) string {
	return buildStaticContext(config)
}
