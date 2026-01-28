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
**PR Context (for background only, do NOT base your review solely on this):**
`
		if prContext.Title != "" {
			prContextSection += fmt.Sprintf("- Title: %s\n", prContext.Title)
		}
		if prContext.Body != "" {
			// Truncate body if too long
			body := prContext.Body
			if len(body) > 500 {
				body = body[:500] + "..."
			}
			prContextSection += fmt.Sprintf("- Description: %s\n", body)
		}
		if len(prContext.CommitMessages) > 0 {
			// Format as a list for better LLM understanding
			prContextSection += "- Commits in this PR:\n"
			for _, msg := range prContext.CommitMessages {
				prContextSection += fmt.Sprintf("  * %s\n", msg)
			}
		}
		prContextSection += "(This context is supplementary. Focus your review on the actual diff below.)\n"
	}

	return fmt.Sprintf(`You are a senior software engineer conducting a code review.
Your goal is to review the provided git diff and provide actionable, specific feedback.

%s

**Diff (Changes to Review):**
%s

**Full Content of Changed Files (Complete Context):**
%s

**Related Dependency Files (Requested Context):**
%s

**Repository Structure (File Tree):**
%s

**Focus Areas:**
%v

**Rules:**
1. Review ONLY the changed lines in the diff. However, if you spot a CRITICAL bug in the related dependency files that affects the changes, include it in your comments.
2. Be specific and actionable. Suggest fixes where possible.
3. Max 2 short sentences per comment.
4. No explanations or praise (e.g., "Good job", "This looks correct").
5. Only problem + fix suggestion in the comment message.
6. Output MUST be valid JSON matching the schema below.
7. If no issues are found, return an empty "comments" list.

**Output Schema (JSON):**
{
  "summary": "Brief summary of the review (1 sentence)",
  "comments": [
    {
      "file": "relative/path/to/file.go",
      "line": 42,
      "layer": "security|bug|lint|performance|architecture",
      "message": "Clear explanation with fix suggestion",
      "severity": "critical|warning|info"
    }
  ]
}
`, prContextSection, diff, changedContent, depsContent, repoStructure, layers)
}

// AnalyzeDependencyNeeds asks the LLM which other files are needed for context.
func AnalyzeDependencyNeeds(ctx context.Context, client *http.Client, diff string, changedFiles map[string]string, repoStructure string, prContext models.PRContext, apiKey string) ([]string, error) {
	// Construct Prompt
	var fileList []string
	for path := range changedFiles {
		fileList = append(fileList, path)
	}

	prompt := fmt.Sprintf(`You are a senior software engineer planning a code review.
Your goal is to identify which *additional* files from the repository you need to read to fully understand and validate the changes.

**Input Diff:**
%s

**Changed Files List:**
%v

**Repository Structure:**
%s

**Task:**
Analyze the diff and changed files (imports, function calls, type usage).
Return a JSON list of file paths that are NOT in the "Changed Files List" but are CRITICAL for verifying the correctness of the changes (e.g., definitions of used types, updated interfaces, middleware logic).
Do not request standard library files or external dependencies.
Only request files that exist in the "Repository Structure".

**Output Schema (JSON):**
{
  "files": ["path/to/file1.go", "path/to/file2.ts"]
}
If no extra files are needed, return {"files": []}.
`, diff, fileList, repoStructure)

	if prContext.Title != "" {
		prompt += fmt.Sprintf("\nReview Context from PR: %s\n%s", prContext.Title, prContext.Body)
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
