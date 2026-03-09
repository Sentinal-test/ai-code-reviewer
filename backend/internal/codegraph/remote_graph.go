package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RemoteRepoGraph holds the lightweight code graph for an auxiliary repository.
// It is optimized for cross-repo matching without storing full dependency trees.
type RemoteRepoGraph struct {
	RepoFullName  string                     `json:"repo_full_name"`
	DefaultBranch string                     `json:"default_branch,omitempty"`
	CommitSHA     string                     `json:"commit_sha"`
	IndexedAt     time.Time                  `json:"indexed_at"`
	Files         map[string]RemoteFileEntry `json:"files"`
}

// RemoteFileEntry contains the extracted metadata for a single remote source file.
type RemoteFileEntry struct {
	Language    string       `json:"language"`
	Definitions []Definition `json:"definitions"`
	Imports     []string     `json:"imports,omitempty"`
	// Placeholders for future advanced extraction:
	// HTTPRoutes []string
	// EventTopics []string
	// gRPCServices []string
}

// BuildRemoteGraph creates a lightweight graph representation of the repository at rootDir.
// It uses existing parsers to extract only definitions and imports.
func BuildRemoteGraph(ctx context.Context, repoFullName string, rootDir string) (*RemoteRepoGraph, error) {
	graph := &RemoteRepoGraph{
		RepoFullName: repoFullName,
		IndexedAt:    time.Now(),
		Files:        make(map[string]RemoteFileEntry),
	}

	// Fetch current commit SHA if .git is present
	graph.CommitSHA = GetCurrentCommitSHA(rootDir)

	var files []string
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(rootDir, path)
		if err == nil {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan remote repo %s: %w", repoFullName, err)
	}

	parser := NewParser()

	for _, path := range files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		langConfig, ok := GetLanguageForFile(path)
		if !ok || langConfig == nil {
			continue // Skip unsupported file types
		}

		fullPath := filepath.Join(rootDir, path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		rootNode, err := parser.ParseFile(ctx, content, langConfig)
		if err != nil || rootNode == nil {
			fmt.Printf("BuildRemoteGraph Error ParseFile %s: %v\n", path, err)
			continue
		}

		langName := strings.ToLower(langConfig.Name)
		defs, err := parser.ExtractDefinitionEntries(rootNode, content, langName)
		if err != nil {
			fmt.Printf("BuildRemoteGraph Error ExtractDefinitionEntries %s: %v\n", path, err)
			continue
		}

		imports := parser.ExtractImports(rootNode, content, langName)

		// Filter for exported/public symbols depending on language logic if desired.
		// For now we store all parser.ExtractDefinitionEntries which might be broad, but is acceptable.
		graph.Files[path] = RemoteFileEntry{
			Language:    langName,
			Definitions: defs,
			Imports:     imports,
		}
	}

	return graph, nil
}
