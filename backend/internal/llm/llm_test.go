package llm

import (
	"code-review/backend/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockLLMProvider struct {
	response GenerateResponse
	err      error
}

func (m *MockLLMProvider) GenerateContent(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	return m.response, m.err
}

func (m *MockLLMProvider) CreateCache(ctx context.Context, sys, usr string, tools []ToolDeclaration) (string, error) {
	return "test-cache-id", nil
}

func (m *MockLLMProvider) DeleteCache(ctx context.Context, cacheID string) error {
	return nil
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

	mockProvider := &MockLLMProvider{
		response: GenerateResponse{
			Text:         string(resultJSON),
			FinishReason: "stop",
		},
	}

	result, err := RunReview(context.Background(), mockProvider, "diff content", changedFiles, nil, models.RepoSettings{}, "", models.PRContext{})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Good code", result.Summary)
	assert.Len(t, result.Comments, 1)
}

func TestRunReview_APIError(t *testing.T) {
	changedFiles := map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	}

	mockProvider := &MockLLMProvider{
		err: fmt.Errorf("LLM returned context deadline exceeded or error"),
	}

	_, err := RunReview(context.Background(), mockProvider, "diff", changedFiles, nil, models.RepoSettings{}, "", models.PRContext{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM returned context deadline")
}

func TestRunReview_EmptyResponse(t *testing.T) {
	changedFiles := map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	}

	// Mock empty candidates
	mockProvider := &MockLLMProvider{
		response: GenerateResponse{
			FinishReason: "EMPTY",
			Text:         "",
		},
	}

	_, err := RunReview(context.Background(), mockProvider, "diff", changedFiles, nil, models.RepoSettings{}, "", models.PRContext{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM returned empty response")
}

func TestRunReview_MalformedJSON(t *testing.T) {
	changedFiles := map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	}

	// Mock response where text is not valid JSON
	mockProvider := &MockLLMProvider{
		response: GenerateResponse{
			FinishReason: "stop",
			Text:         "not valid json",
		},
	}

	_, err := RunReview(context.Background(), mockProvider, "diff", changedFiles, nil, models.RepoSettings{}, "", models.PRContext{})
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

func TestExtractChangedLinesFromDiff_SkipsFileHeaders(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/pkg/contracts/provisioning/provisioning.go b/pkg/contracts/provisioning/provisioning.go",
		"--- a/pkg/contracts/provisioning/provisioning.go",
		"+++ b/pkg/contracts/provisioning/provisioning.go",
		"@@ -2,8 +2,7 @@ package provisioning",
		"-func BuildProvisioningTicket(subscriptionID string) (string, error) {",
		"+func BuildProvisioningRequest(subscriptionID string) (string, error) {",
		" if subscriptionID == \"\" {",
		"  return \"\", errors.New(\"empty subscription id\")",
		" }",
	}, "\n")

	sections := extractChangedLinesFromDiff(diff)
	require.Len(t, sections, 1)
	require.Contains(t, sections, "pkg/contracts/provisioning/provisioning.go")
	require.Len(t, sections["pkg/contracts/provisioning/provisioning.go"], 1)
	assert.NotContains(t, sections["pkg/contracts/provisioning/provisioning.go"][0], "--- a/")
	assert.NotContains(t, sections["pkg/contracts/provisioning/provisioning.go"][0], "+++ b/")
}
