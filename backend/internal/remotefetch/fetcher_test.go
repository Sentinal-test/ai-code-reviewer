package remotefetch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchSnippet(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fetch-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "org", "repo")
	os.MkdirAll(repoDir, 0755)

	content := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5\nLine 6\nLine 7\nLine 8\nLine 9\nLine 10"
	os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte(content), 0644)

	fetcher := NewFetcher(tmpDir, 5, 5, 1000)

	// Test 1: Happy path
	snippet, err := fetcher.FetchSnippet(context.Background(), "org/repo", "test.txt", 2, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snippet != "Line 2\nLine 3\nLine 4" {
		t.Errorf("unexpected snippet: %q", snippet)
	}

	// Test 2: Clamp max lines
	snippet2, err := fetcher.FetchSnippet(context.Background(), "org/repo", "test.txt", 1, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(snippet2, "\n")
	if len(lines) != 6 { // startLine + 5 = elements 1..6
		t.Errorf("expected max line clamping to 6 lines, got %d", len(lines))
	}

	// Test 3: Fetch limits
	for i := 0; i < 3; i++ {
		_, _ = fetcher.FetchSnippet(context.Background(), "org/repo", "test.txt", 1, 2)
	}
	_, err = fetcher.FetchSnippet(context.Background(), "org/repo", "test.txt", 1, 2)
	if err == nil || !strings.Contains(err.Error(), "remote fetch budget exceeded") {
		t.Errorf("expected fetch budget exceeded error, got: %v", err)
	}
}
