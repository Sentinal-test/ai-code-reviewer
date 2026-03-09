package remotefetch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Fetcher manages fetching exact snippets of remote repositories
// and tracks a budget to ensure we don't blow up context windows.
type Fetcher struct {
	baseTempDir string
	maxLines    int
	maxBytes    int

	mu         sync.Mutex
	usedBytes  int
	fetchCount int
	maxFetches int
}

// NewFetcher initializes a snippet fetcher with configurable constraints.
func NewFetcher(baseTempDir string, maxFetches, maxLines, maxBytes int) *Fetcher {
	return &Fetcher{
		baseTempDir: baseTempDir,
		maxFetches:  maxFetches,
		maxLines:    maxLines,
		maxBytes:    maxBytes,
	}
}

// FetchSnippet fetches a limited number of lines from a specific file
// in the cloned remote repository.
func (f *Fetcher) FetchSnippet(ctx context.Context, repoFullName string, filePath string, startLine, endLine int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fetchCount >= f.maxFetches {
		return "", fmt.Errorf("remote fetch budget exceeded: max %d fetches allowed", f.maxFetches)
	}
	if f.usedBytes >= f.maxBytes {
		return "", fmt.Errorf("remote byte budget exceeded: max %d bytes allowed", f.maxBytes)
	}

	// 1-indexed to 0-indexed alignment
	if startLine < 1 {
		startLine = 1
	}
	if endLine < startLine {
		endLine = startLine + 50 // sensible default window if endLine is misconfigured
	}
	if endLine-startLine > f.maxLines {
		endLine = startLine + f.maxLines
	}

	parts := strings.SplitN(repoFullName, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid repo full name %s", repoFullName)
	}

	fullPath := filepath.Join(f.baseTempDir, parts[0], parts[1], filePath)

	contentBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read remote file %s in %s: %w", filePath, repoFullName, err)
	}

	lines := strings.Split(string(contentBytes), "\n")

	// Boundary clamps
	if startLine > len(lines) {
		return "", fmt.Errorf("start line %d exceeds file length %d", startLine, len(lines))
	}

	actualEnd := endLine
	if actualEnd > len(lines) {
		actualEnd = len(lines)
	}

	snippet := strings.Join(lines[startLine-1:actualEnd], "\n")

	f.usedBytes += len(snippet)
	f.fetchCount++

	return snippet, nil
}
