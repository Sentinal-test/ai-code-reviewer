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
	if !strings.Contains(snippet, "Repo: org/repo") || !strings.Contains(snippet, "2: Line 2\n3: Line 3\n4: Line 4") {
		t.Errorf("unexpected snippet: %q", snippet)
	}

	// Test 2: Clamp max lines
	snippet2, err := fetcher.FetchSnippet(context.Background(), "org/repo", "test.txt", 1, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(snippet2, "Lines: 1-5") {
		t.Errorf("expected max line clamping to line 5, got %q", snippet2)
	}

	// Test 3: Fetch limits
	for i := 0; i < 3; i++ {
		_, _ = fetcher.FetchSnippet(context.Background(), "org/repo", "test.txt", 1, 2)
	}
	_, err = fetcher.FetchSnippet(context.Background(), "org/repo", "test.txt", 1, 2)
	if err == nil || !strings.Contains(err.Error(), "remote fetch budget exceeded") {
		t.Errorf("expected fetch budget exceeded error, got: %v", err)
	}

	// Test 4: Path traversal rejected
	safeFetcher := NewFetcher(tmpDir, 5, 5, 1000)
	_, err = safeFetcher.FetchSnippet(context.Background(), "org/repo", "../repo/test.txt", 1, 2)
	if err == nil || !strings.Contains(err.Error(), "invalid remote file path") {
		t.Errorf("expected invalid remote file path error, got: %v", err)
	}
}
