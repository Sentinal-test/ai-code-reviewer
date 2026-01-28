package llm

import (
	"bytes"
	"code-review/backend/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	geminiModel = "gemini-2.5-flash"
	geminiURL   = "https://generativelanguage.googleapis.com/v1beta/models/" + geminiModel + ":generateContent"
)

// RunReview analyzes the diff using the provided API key, settings, and PR context.
func RunReview(ctx context.Context, client *http.Client, diff string, changedFiles map[string]string, dependencies map[string]string, settings models.RepoSettings, repoStructure string, apiKey string, prContext models.PRContext) (*models.ReviewResult, error) {
	// 1. Construct Prompt
	prompt := buildPrompt(diff, changedFiles, dependencies, settings, repoStructure, prContext)

	// 2. Prepare Request
	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"responseMimeType": "application/json",
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	url := fmt.Sprintf("%s?key=%s", geminiURL, apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 3. Execute Request
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(body))
	}

	// 4. Parse Response
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %v", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("LLM returned empty response")
	}

	responseText := geminiResp.Candidates[0].Content.Parts[0].Text

	// Robust parsing: try Result object first, then fallback to Array of comments
	var result models.ReviewResult
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		// Fallback: Check if it's a naked array of comments
		var comments []models.ReviewComment
		if errArray := json.Unmarshal([]byte(responseText), &comments); errArray == nil {
			result.Comments = comments
			result.Summary = "Automated review comments"
		} else {
			return nil, fmt.Errorf("failed to unmarshal JSON content: %v | content: %s", err, responseText)
		}
	}

	return &result, nil
}

func buildPrompt(diff string, changedFiles map[string]string, dependencies map[string]string, settings models.RepoSettings, repoStructure string, prContext models.PRContext) string {
	// Context Window Management (Simple implementation)
	// Priority: Diff > Changed Files > Dependencies > Repo Structure
	// Target Max Chars: ~400,000 (approx 100k tokens safety)
	const MaxContextChars = 400000

	currentSize := len(diff)

	// Helper to format map
	formatFiles := func(files map[string]string) string {
		var b strings.Builder
		for path, content := range files {
			b.WriteString(fmt.Sprintf("\n--- FILE: %s ---\n%s\n", path, content))
		}
		return b.String()
	}

	changedContent := formatFiles(changedFiles)
	if currentSize+len(changedContent) > MaxContextChars {
		// Truncate changed files
		available := MaxContextChars - currentSize
		if available > 0 {
			if len(changedContent) > available {
				changedContent = changedContent[:available] + "\n...[TRUNCATED]..."
			}
		} else {
			changedContent = ""
		}
	}
	currentSize += len(changedContent)

	depsContent := formatFiles(dependencies)
	if currentSize+len(depsContent) > MaxContextChars {
		available := MaxContextChars - currentSize
		if available > 0 {
			if len(depsContent) > available {
				depsContent = depsContent[:available] + "\n...[TRUNCATED]..."
			}
		} else {
			depsContent = ""
		}
	}
	currentSize += len(depsContent)

	// Repo structure
	if currentSize+len(repoStructure) > MaxContextChars {
		available := MaxContextChars - currentSize
		if available > 0 {
			if len(repoStructure) > available {
				repoStructure = repoStructure[:available] + "\n...[TRUNCATED]..."
			}
		} else {
			repoStructure = ""
		}
	}

	var layers = []string{}
	if settings.SecurityEnabled {
		layers = append(layers, "Security (vulnerabilities, secrets)")
	}
	if settings.BugEnabled {
		layers = append(layers, "Bugs (logic errors, crashes)")
	}
	if settings.LintEnabled {
		layers = append(layers, "Lint (style, formatting, best practices)")
	}
	if settings.PerformanceEnabled {
		layers = append(layers, "Performance (inefficiencies, memory leaks)")
	}
	if settings.ArchitectureEnabled {
		layers = append(layers, "Architecture (design patterns, structure)")
	}

	// Build PR context section (optional, for extra knowledge)
	var prContextSection string
	if prContext.Title != "" || prContext.Body != "" || len(prContext.CommitMessages) > 0 {
		prContextSection = `
**PR CONTEXT (Background Information - Use for Understanding Intent Only):**
`
		if prContext.Title != "" {
			prContextSection += fmt.Sprintf("- PR Title: %s\n", prContext.Title)
		}
		if prContext.Body != "" {
			// Truncate body if too long
			body := prContext.Body
			if len(body) > 500 {
				body = body[:500] + "..."
			}
			prContextSection += fmt.Sprintf("- PR Description: %s\n", body)
		}
		if len(prContext.CommitMessages) > 0 {
			// Format as a list for better LLM understanding
			prContextSection += "- Recent Commits (last 10):\n"
			msgs := prContext.CommitMessages
			if len(msgs) > 10 {
				msgs = msgs[len(msgs)-10:]
			}
			for _, msg := range msgs {
				prContextSection += fmt.Sprintf("  * %s\n", msg)
			}
		}
		prContextSection += "\nIMPORTANT: This context helps you understand what the developer intended. Your review must focus on the actual diff below, identifying problems regardless of intent.\n"
	}

	return fmt.Sprintf(`You are an automated code defect detection system performing static analysis.
Your SOLE objective is to identify defects, vulnerabilities, bugs, and code quality issues in the git diff provided below.

%s

═══════════════════════════════════════════════════════════════════════════════
PRIMARY ANALYSIS TARGET: GIT DIFF (Changes Under Review)
═══════════════════════════════════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════════════════════════════════
SUPPORTING CONTEXT: Complete Changed Files (For Reference)
═══════════════════════════════════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════════════════════════════════
SUPPORTING CONTEXT: Related Dependencies (For Interface/Type Verification)
═══════════════════════════════════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════════════════════════════════
SUPPORTING CONTEXT: Repository Structure (For Architecture Analysis)
═══════════════════════════════════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════════════════════════════════
ACTIVE DETECTION LAYERS
═══════════════════════════════════════════════════════════════════════════════
%v

═══════════════════════════════════════════════════════════════════════════════
MANDATORY ANALYSIS RULES - STRICT COMPLIANCE REQUIRED
═══════════════════════════════════════════════════════════════════════════════

RULE 1 - PRIMARY FOCUS ON DIFF:
  ✓ Analyze ONLY the changed lines in the git diff (lines with + or - prefixes)
  ✓ Each comment MUST reference a specific line number from a changed file
  ✓ DO NOT comment on unchanged code unless Rule 2 applies
  ✗ NEVER comment on code that is not part of the diff

RULE 2 - CRITICAL ISSUES IN CONTEXT FILES:
  ✓ IF you find a CRITICAL security vulnerability or severe bug in dependency/context files
  ✓ AND it DIRECTLY impacts or is called by the changed code
  ✓ THEN report it with: file=<dependency_file>, line=0, message="[CONTEXT] <problem>"
  ✓ Use ONLY for severity: critical or warning
  ✗ DO NOT use for general code suggestions or info-level issues

RULE 3 - ZERO FALSE POSITIVES:
  ✗ DO NOT comment on code that is correct, functional, or follows best practices
  ✗ DO NOT provide praise, confirmations, or acknowledgments ("Good implementation", "This is correct")
  ✗ DO NOT comment on style preferences unless they violate language standards or cause bugs
  ✗ DO NOT provide educational content or explanations
  ✗ DO NOT comment if you cannot identify a concrete, measurable defect
  ✓ Silence on correct code is EXPECTED and DESIRED

RULE 4 - COMMENT STRUCTURE (Strict Format):
  Format: "<Problem>. <Fix>."
  
  ✓ CORRECT Examples:
    - "SQL injection via unsanitized input. Use parameterized queries."
    - "Nil pointer dereference if user is nil. Add nil check before accessing user.ID."
    - "Race condition on shared map access. Use mutex or sync.Map."
    - "Memory leak from unclosed file handle. Defer file.Close() after error check."
  
  ✗ INCORRECT Examples:
    - "This is a good approach" (praise - forbidden)
    - "The code handles errors properly" (confirmation - forbidden)
    - "Consider using a different pattern here" (vague - not actionable)
    - "This might cause issues in some edge cases" (unspecific - not actionable)
  
  Maximum: 2 concise sentences per comment
  
RULE 5 - SEVERITY CLASSIFICATION (Precise Definitions):
  critical: Security vulnerabilities (injection, XSS, auth bypass), data corruption, guaranteed crashes, 
           breaking API changes, exposed secrets/credentials
  
  warning:  Logic errors, potential nil panics, resource leaks, race conditions, incorrect error handling,
           deprecated APIs, improper error propagation, off-by-one errors
  
  info:     Minor inefficiencies, missing non-critical error checks, suboptimal patterns that don't affect 
           correctness, redundant code

RULE 6 - OUTPUT FORMAT (Strict JSON Schema):
  ✓ MUST be valid JSON with no markdown formatting
  ✓ Summary format: "Found X critical, Y warning, Z info issue(s)" (use exact counts)
  ✓ If no issues: {"summary": "No issues detected", "comments": []}
  ✗ NEVER include markdown code fences, extra text, or explanations

RULE 7 - CONTEXT USAGE GUIDELINES:
  - PR title/description/commits → Understand developer intent (what they tried to do)
  - Changed files (full content) → Understand code flow, dependencies, and usage patterns
  - Dependency files → Verify interface contracts, type definitions, function signatures
  - Repository structure → Validate import paths, architecture decisions, module organization
  
  Remember: Context helps you understand the code, but your review must identify actual problems
  in the diff, not validate whether the implementation matches the intent.

═══════════════════════════════════════════════════════════════════════════════
OUTPUT JSON SCHEMA (No markdown, no extra text)
═══════════════════════════════════════════════════════════════════════════════
{
  "summary": "Found <count> critical, <count> warning, <count> info issue(s)" | "No issues detected",
  "comments": [
    {
      "file": "relative/path/to/file.go",
      "line": 42,
      "layer": "security|bug|lint|performance|architecture",
      "message": "<Problem>. <Fix>.",
      "severity": "critical|warning|info"
    }
  ]
}

═══════════════════════════════════════════════════════════════════════════════
FINAL REMINDER
═══════════════════════════════════════════════════════════════════════════════
You are a DEFECT DETECTOR, not a code reviewer or mentor.
- Report ONLY actual problems with concrete fixes
- NO praise, NO confirmations, NO educational content
- Silence on correct code is correct behavior
- Focus on the DIFF, reference context only when necessary

BEGIN ANALYSIS NOW.
`, prContextSection, diff, changedContent, depsContent, repoStructure, layers)
}

// AnalyzeDependencyNeeds asks the LLM which other files are needed for context.
func AnalyzeDependencyNeeds(ctx context.Context, client *http.Client, diff string, changedFiles map[string]string, repoStructure string, prContext models.PRContext, apiKey string) ([]string, error) {
	// Construct Prompt
	var fileList []string
	for path := range changedFiles {
		fileList = append(fileList, path)
	}

	prompt := fmt.Sprintf(`You are a dependency analyzer for automated code review systems.
Your task is to identify which additional repository files are REQUIRED to accurately validate the changes in the provided diff.

═══════════════════════════════════════════════════════════════════════════════
INPUT: GIT DIFF
═══════════════════════════════════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════════════════════════════════
ALREADY AVAILABLE: Changed Files
═══════════════════════════════════════════════════════════════════════════════
%v

═══════════════════════════════════════════════════════════════════════════════
AVAILABLE: Repository Structure
═══════════════════════════════════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════════════════════════════════
ANALYSIS TASK
═══════════════════════════════════════════════════════════════════════════════

Analyze the diff and identify files that are CRITICAL for validating the changes.

INCLUDE files that provide:
✓ Type definitions, struct definitions, or interfaces used in the diff
✓ Function/method signatures that are called by the changed code
✓ Constants, enums, or configuration referenced in the changes
✓ Parent classes, base implementations, or mixins that the changed code extends
✓ Database schema or model definitions if the diff includes queries
✓ API endpoint definitions if the diff implements handlers
✓ Middleware or interceptors that process the changed code's execution flow
✓ Critical utility functions that the changed code depends on

EXCLUDE files that are:
✗ Standard library imports (e.g., "fmt", "encoding/json", "react")
✗ External dependencies from node_modules, vendor, or package managers
✗ Test files unless the diff modifies production code called by those tests
✗ Documentation, README, or configuration files (unless diff validates config)
✗ Files not present in the "Repository Structure" above
✗ Changed files already listed in "ALREADY AVAILABLE" section

PRIORITIZATION (request max 10 files, highest priority first):
1. Direct dependencies: files imported or referenced by changed code
2. Type definitions: interfaces, structs, classes used in the diff
3. Architectural dependencies: middleware, base classes, core utilities

═══════════════════════════════════════════════════════════════════════════════
OUTPUT REQUIREMENTS
═══════════════════════════════════════════════════════════════════════════════

Return ONLY valid JSON matching this schema:
{
  "files": ["path/to/file1.go", "path/to/file2.ts"]
}

Rules:
- Return empty array if no additional files are needed: {"files": []}
- Maximum 10 files (prioritize most critical)
- Paths must exactly match those in "Repository Structure"
- No markdown formatting, no explanations, only JSON

BEGIN ANALYSIS NOW.
`, diff, fileList, repoStructure)

	// Optionally add PR context if available
	if prContext.Title != "" {
		prompt += fmt.Sprintf("\n\nPR Context (for understanding intent):\nTitle: %s\nDescription: %s", prContext.Title, prContext.Body)
	}

	// Prepare Request
	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"responseMimeType": "application/json",
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s?key=%s", geminiURL, apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM scout failed status %d: %s", resp.StatusCode, string(body))
	}

	// Parse
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, err
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return []string{}, nil
	}

	responseText := geminiResp.Candidates[0].Content.Parts[0].Text

	// Extraction logic to handle markdown backticks if LLM provides them
	jsonStr := responseText
	if idx := strings.Index(jsonStr, "```json"); idx != -1 {
		jsonStr = jsonStr[idx+7:]
		if endIdx := strings.Index(jsonStr, "```"); endIdx != -1 {
			jsonStr = jsonStr[:endIdx]
		}
	} else if idx := strings.Index(jsonStr, "```"); idx != -1 {
		jsonStr = jsonStr[idx+3:]
		if endIdx := strings.Index(jsonStr, "```"); endIdx != -1 {
			jsonStr = jsonStr[:endIdx]
		}
	}
	jsonStr = strings.TrimSpace(jsonStr)

	var result struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		fmt.Printf("⚠️ Scout JSON Parse Failed: %v | Response: %s\n", err, responseText)
		return nil, fmt.Errorf("failed to parse scout JSON: %v", err)
	}

	return result.Files, nil
}