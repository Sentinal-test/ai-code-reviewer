package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	Symbols       map[string]Symbol          `json:"symbols,omitempty"`
	SymbolEdges   []SymbolEdge               `json:"symbol_edges,omitempty"`
	PackageDeps   []PackageDependency        `json:"package_deps,omitempty"`

	// InvertedReferences accelerates remote caller lookups. It is rebuilt at index time.
	InvertedReferences map[string][]SymbolLocation `json:"-"`
}

// RemoteFileEntry contains the extracted metadata for a single remote source file.
type RemoteFileEntry struct {
	Language      string            `json:"language"`
	Package       string            `json:"package,omitempty"`
	Definitions   []Definition      `json:"definitions"`
	References    []Reference       `json:"references,omitempty"`
	Imports       []string          `json:"imports,omitempty"`
	ImportAliases map[string]string `json:"import_aliases,omitempty"`
}

// BuildRemoteGraph creates a symbol-aware graph representation of a peer repository.
// It preserves internal symbol/package edges and keeps high-signal unresolved external
// references so multi-repo matching can identify real downstream consumers.
func BuildRemoteGraph(ctx context.Context, repoFullName string, rootDir string) (*RemoteRepoGraph, error) {
	remote := &RemoteRepoGraph{
		RepoFullName: repoFullName,
		IndexedAt:    time.Now(),
		Files:        make(map[string]RemoteFileEntry),
		Symbols:      make(map[string]Symbol),
	}

	graph := NewGraph()
	graph.CommitSHA = GetCurrentCommitSHA(rootDir)
	graph.IndexedAt = remote.IndexedAt

	var files []string
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || skipDirs[info.Name()] {
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
		if isTestFile(path) {
			continue
		}

		fullPath := filepath.Join(rootDir, path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		entry, err := parseFileEntry(ctx, parser, content, langConfig, path)
		if err != nil {
			fmt.Printf("BuildRemoteGraph Error parseFileEntry %s: %v\n", path, err)
			continue
		}
		graph.Files[path] = *entry
	}

	resolveRemoteReferences(graph, rootDir)
	rebuildGraphIndexes(graph, rootDir)

	remote.CommitSHA = graph.CommitSHA
	remote.Symbols = graph.Symbols
	remote.SymbolEdges = graph.SymbolEdges
	remote.PackageDeps = graph.PackageDeps
	remote.InvertedReferences = graph.InvertedReferences
	for path, entry := range graph.Files {
		remote.Files[path] = RemoteFileEntry{
			Language:      entry.Language,
			Package:       entry.Package,
			Definitions:   entry.Definitions,
			References:    entry.References,
			Imports:       entry.Imports,
			ImportAliases: entry.ImportAliases,
		}
	}

	return remote, nil
}

func resolveRemoteReferences(g *Graph, repoPath string) {
	if g == nil {
		return
	}

	lookup := buildSymbolLookup(g)
	goModulePath := loadGoModulePath(repoPath)

	for path, entry := range g.Files {
		filtered := make([]Reference, 0, len(entry.References))
		for _, ref := range entry.References {
			resolved, keep := resolveReference(g, path, entry, ref, lookup, repoPath, goModulePath)
			if keep {
				filtered = append(filtered, resolved)
				continue
			}
			if keepRemoteExternalReference(entry, ref) {
				filtered = append(filtered, ref)
			}
		}
		entry.References = filtered
		g.Files[path] = entry
	}
}

func keepRemoteExternalReference(entry FileEntry, ref Reference) bool {
	if ref.Symbol == "" || isBuiltinSymbol(ref.Symbol) {
		return false
	}

	if ref.Qualifier != "" {
		importPath, ok := entry.ImportAliases[ref.Qualifier]
		if !ok {
			return false
		}
		return !IsStandardLibraryImport(entry.Language, importPath)
	}

	if importPath, ok := entry.ImportAliases[ref.Symbol]; ok {
		return !IsStandardLibraryImport(entry.Language, importPath)
	}

	return false
}
