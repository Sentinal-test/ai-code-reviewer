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
func RunReview(ctx context.Context, client *http.Client, diff string, settings models.RepoSettings, apiKey string, prContext models.PRContext) (*models.ReviewResult, error) {
	// 1. Construct Prompt
	prompt := buildPrompt(diff, settings, prContext)

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

	var result models.ReviewResult
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON content: %v | content: %s", err, responseText)
	}

	return &result, nil
}

func buildPrompt(diff string, settings models.RepoSettings, prContext models.PRContext) string {
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
			// Only include first 10 commits to avoid bloat
			msgs := prContext.CommitMessages
			if len(msgs) > 10 {
				msgs = msgs[:10]
			}
			prContextSection += fmt.Sprintf("- Commits: %s\n", strings.Join(msgs, "; "))
		}
		prContextSection += "(This context is supplementary. Focus your review on the actual diff below.)\n"
	}

	return fmt.Sprintf(`You are a senior software engineer conducting a code review.
Your goal is to review the provided git diff and provide actionable, specific feedback.
%s
**Focus Areas:**
%v

**Rules:**
1. Review ONLY the changed lines in the diff.
2. Be specific and actionable. Suggest fixes where possible.
3. Max 2 short sentences per comment.
4. No explanations or praise (e.g., "Good job", "This looks correct").
5. Only problem + fix suggestion in the comment message.
6. Output MUST be valid JSON matching the schema below.
7. If no issues are found, return an empty "comments" list.

**Input Diff:**
%s

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
`, prContextSection, layers, diff)
}
