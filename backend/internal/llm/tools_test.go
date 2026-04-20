package llm

import (
	"os"
	"strings"
	"testing"
)

func TestGetFileContentRangeIncludesLineNumbersAndTruncatesWindow(t *testing.T) {
	te := &ToolExecutor{RepoPath: t.TempDir()}

	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, "line content")
	}
	path := writeTempFile(t, te.RepoPath, "sample.go", strings.Join(lines, "\n"))

	resp := te.getFileContent(map[string]interface{}{
		"path":       path,
		"start_line": float64(50),
		"end_line":   float64(500),
	})

	if !strings.Contains(resp.Content, "50: line content") {
		t.Fatalf("expected ranged response to include line numbers, got %q", resp.Content[:min(len(resp.Content), 120)])
	}
	if !strings.Contains(resp.Content, "449: line content") {
		t.Fatalf("expected ranged response to be capped at 400 lines")
	}
	if strings.Contains(resp.Content, "500: line content") {
		t.Fatalf("expected ranged response to truncate long windows")
	}
	if !strings.Contains(resp.Content, "truncated to 400 lines") {
		t.Fatalf("expected truncation notice in ranged response")
	}
}

func TestGetFileContentFullFileIsCapped(t *testing.T) {
	te := &ToolExecutor{RepoPath: t.TempDir()}

	path := writeTempFile(t, te.RepoPath, "big.go", strings.Repeat("abcdefghij", 2000))
	resp := te.getFileContent(map[string]interface{}{"path": path})

	if !strings.Contains(resp.Content, "truncated at 12K chars") {
		t.Fatalf("expected capped full-file response, got %q", resp.Content[len(resp.Content)-80:])
	}
}

func TestAgentToolDeclarationsExposeFileWindowParameters(t *testing.T) {
	decls := AgentToolDeclarations()
	for _, decl := range decls {
		if decl.Name != "get_file_content" {
			continue
		}
		props, _ := decl.Parameters["properties"].(map[string]interface{})
		if _, ok := props["start_line"]; !ok {
			t.Fatalf("expected get_file_content tool to expose start_line")
		}
		if _, ok := props["end_line"]; !ok {
			t.Fatalf("expected get_file_content tool to expose end_line")
		}
		return
	}
	t.Fatalf("get_file_content tool declaration not found")
}

func writeTempFile(t *testing.T, root, relPath, content string) string {
	t.Helper()
	fullPath := root + "/" + relPath
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return relPath
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
