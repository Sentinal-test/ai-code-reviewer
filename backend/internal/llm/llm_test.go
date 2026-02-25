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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockRoundTripper struct {
	RoundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *MockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.RoundTripFunc(req)
}

func TestRunReview_Success(t *testing.T) {
	changedFiles := map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	}

	// Prepare expected review result
	expectedResult := models.ReviewResult{
		Summary: "Good code",
		Comments: []models.ReviewComment{
			{File: "main.go", Line: 10, Message: "Fix this", Severity: "warning"},
		},
	}
	resultJSON, _ := json.Marshal(expectedResult)

	// Mock Gemini response structure
	geminiRespObj := map[string]interface{}{
		"candidates": []map[string]interface{}{
			{
				"content": map[string]interface{}{
					"parts": []map[string]interface{}{
						{"text": string(resultJSON)},
					},
				},
			},
		},
	}
	geminiRespBytes, _ := json.Marshal(geminiRespObj)

	mockClient := &http.Client{
		Transport: &MockRoundTripper{
			RoundTripFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request
				assert.Equal(t, "POST", req.Method)
				assert.Contains(t, req.URL.String(), geminiURL) // basic check
				assert.Contains(t, req.URL.String(), "key=test-api-key")

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer(geminiRespBytes)),
					Header:     make(http.Header),
				}, nil
			},
		},
	}

	result, err := RunReview(context.Background(), mockClient, "diff content", changedFiles, nil, models.RepoSettings{}, "", "test-api-key", models.PRContext{})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Good code", result.Summary)
	assert.Len(t, result.Comments, 1)
}

func TestRunReview_APIError(t *testing.T) {
	changedFiles := map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	}

	mockClient := &http.Client{
		Transport: &MockRoundTripper{
			RoundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(bytes.NewBufferString("Internal Server Error")),
					Header:     make(http.Header),
				}, nil
			},
		},
	}

	_, err := RunReview(context.Background(), mockClient, "diff", changedFiles, nil, models.RepoSettings{}, "", "key", models.PRContext{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM returned status 500")
}

func TestRunReview_EmptyResponse(t *testing.T) {
	changedFiles := map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	}

	// Mock empty candidates
	geminiRespObj := map[string]interface{}{
		"candidates": []interface{}{},
	}
	geminiRespBytes, _ := json.Marshal(geminiRespObj)

	mockClient := &http.Client{
		Transport: &MockRoundTripper{
			RoundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer(geminiRespBytes)),
					Header:     make(http.Header),
				}, nil
			},
		},
	}

	_, err := RunReview(context.Background(), mockClient, "diff", changedFiles, nil, models.RepoSettings{}, "", "key", models.PRContext{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM returned empty response")
}

func TestRunReview_MalformedJSON(t *testing.T) {
	changedFiles := map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	}

	// Mock response where text is not valid JSON
	geminiRespObj := map[string]interface{}{
		"candidates": []map[string]interface{}{
			{
				"content": map[string]interface{}{
					"parts": []map[string]interface{}{
						{"text": "not valid json"},
					},
				},
			},
		},
	}
	geminiRespBytes, _ := json.Marshal(geminiRespObj)

	mockClient := &http.Client{
		Transport: &MockRoundTripper{
			RoundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer(geminiRespBytes)),
					Header:     make(http.Header),
				}, nil
			},
		},
	}

	_, err := RunReview(context.Background(), mockClient, "diff", changedFiles, nil, models.RepoSettings{}, "", "key", models.PRContext{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal JSON content")
}
func TestIsDocumentationFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		// Regular code
		{"main.go", false},
		{"internal/logic/service.go", false},
		{"pkg/api/handler.py", false},

		// Documentation
		{"README.md", true},
		{"docs/manual.txt", true},
		{"CHANGELOG", true},
		{"TODO.md", true},

		// Config
		{"go.mod", true},
		{"package.json", true},
		{".gitignore", true},
		{"Makefile", true},

		// Ignored Directories (Added)
		{"node_modules/lodash/index.js", true},
		{"frontend/node_modules/react/index.js", true},
		{"vendor/github.com/stretchr/testify/assert.go", true},
		{"dist/bundle.js", true},
		{"build/logs/output.txt", true},
		{"bin/app.exe", true},
		{"target/debug/app", true},
		{"out/main.o", true},
		{".git/config", true},
		{".ai-reviewer/graph.json", true},
		{".vscode/settings.json", true},
		{".idea/workspace.xml", true},

		// Edge cases (should NOT be ignored)
		{"src/vendor_logic.go", false}, // Contains vendor but not as a directory segment
		{"my_dist_config.go", false},
		{"build_helper.sh", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, isDocumentationFile(tt.path))
		})
	}
}

func TestExtractDiffWithContext(t *testing.T) {
	// Build a synthetic file: package + imports + 200 lines of code
	var fileContent strings.Builder
	fileContent.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n")
	for i := 8; i <= 200; i++ {
		fileContent.WriteString(fmt.Sprintf("// line %d\n", i))
	}
	content := fileContent.String()

	t.Run("single hunk with context", func(t *testing.T) {
		// Diff hunk says change starts at line 100, 3 lines long
		diffSections := []string{"@@ -98,3 +100,3 @@\n-old\n+new"}
		result := extractDiffWithContext(content, diffSections, 5)

		// Should contain import block (lines 1-6)
		assert.Contains(t, result, "1: package main")
		assert.Contains(t, result, `"fmt"`)
		assert.Contains(t, result, `"os"`)

		// Should contain context around line 100 (±5 lines = lines 95-108)
		assert.Contains(t, result, "95:")
		assert.Contains(t, result, "105:")

		// Should have omission markers
		assert.Contains(t, result, "omitted")
	})

	t.Run("no diff sections returns imports only", func(t *testing.T) {
		result := extractDiffWithContext(content, nil, 5)

		// Should contain import block
		assert.Contains(t, result, "1: package main")
		assert.Contains(t, result, `"fmt"`)

		// Should have omission marker
		assert.Contains(t, result, "no diff hunks found")
	})

	t.Run("adjacent hunks are merged", func(t *testing.T) {
		// Two hunks close together: line 50 and line 55 with context=5 → should merge
		diffSections := []string{
			"@@ -48,3 +50,3 @@\n-old1\n+new1",
			"@@ -53,3 +55,3 @@\n-old2\n+new2",
		}
		result := extractDiffWithContext(content, diffSections, 5)

		// Both hunks should be in one continuous block (no omission between them)
		lines := strings.Split(result, "\n")
		omissionCount := 0
		for _, line := range lines {
			if strings.Contains(line, "omitted") {
				omissionCount++
			}
		}
		// Should have at most 2 omission markers (before and after the merged block),
		// not 3 (which would mean the hunks weren't merged)
		assert.LessOrEqual(t, omissionCount, 3,
			"Adjacent hunks should be merged, reducing omission markers")
	})
}

func TestFormatSafeDiff(t *testing.T) {
	input := "@@ -261,4 +262,4 @@\n" +
		" 					},\n" +
		" 					map[string]interface{}{\n" +
		"-\t\t\t\t\t\t\"role\": \"user\",\n" +
		"+\t\t\t\t\t\t\"role\": \"function\",\n" +
		" 					},"

	expected := "@@ -261,4 +262,4 @@\n" +
		"                   \t\t\t\t\t},\n" +
		"                   \t\t\t\t\tmap[string]interface{}{\n" +
		"[OLD_REMOVED_CODE] \t\t\t\t\t\t\"role\": \"user\",\n" +
		"[NEW_LIVE_CODE]    \t\t\t\t\t\t\"role\": \"function\",\n" +
		"                   \t\t\t\t\t},\n"

	result := formatSafeDiff(input)
	assert.Equal(t, expected, result)
}
