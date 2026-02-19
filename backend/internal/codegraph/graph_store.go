package codegraph

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const graphFileName = "graph.json"

// skipDirs contains directories to exclude from graph indexing.
// Covers common non-project directories across all supported languages:
//   - Go:      vendor, testdata
//   - Python:  __pycache__, .venv, venv, site-packages
//   - JS/TS:   node_modules, dist, build, .next, coverage
//   - Java:    target, .gradle, .mvn, out
//   - General: datasets, fixtures, test_data, examples
var skipDirs = map[string]bool{
	// General build/dependency dirs
	"vendor": true, "node_modules": true, "dist": true, "build": true,

	// Go
	"testdata": true,

	// Python
	"__pycache__": true, "venv": true, "site-packages": true,
	".eggs": true, ".tox": true,

	// JavaScript / TypeScript
	".next": true, "coverage": true, ".nuxt": true, ".output": true,

	// Java
	"target": true, "out": true,

	// Test/fixture data (all langs)
	"datasets": true, "fixtures": true, "test_data": true,
	"__tests__": true, "__mocks__": true, "__snapshots__": true,
}

// SaveGraph serializes the graph to a JSON file in the given directory.
func SaveGraph(g *Graph, dir string) error {
	if g == nil {
		return fmt.Errorf("cannot save nil graph")
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create graph directory: %w", err)
	}

	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal graph: %w", err)
	}

	path := filepath.Join(dir, graphFileName)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write graph file: %w", err)
	}

	fmt.Printf("💾 [CodeGraph] Saved graph to %s (%d files, %d definitions, %d edges, %d bytes)\n",
		path, g.FileCount(), g.DefinitionCount(), len(g.Edges), len(data))
	return nil
}

// LoadGraph reads the graph from a JSON file in the given directory.
// Returns nil, nil if the file does not exist (cache miss).
func LoadGraph(dir string) (*Graph, error) {
	path := filepath.Join(dir, graphFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // cache miss — not an error
		}
		return nil, fmt.Errorf("failed to read graph file: %w", err)
	}

	var g Graph
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("failed to unmarshal graph: %w", err)
	}

	// Ensure Files map is initialized
	if g.Files == nil {
		g.Files = make(map[string]FileEntry)
	}

	fmt.Printf("📂 [CodeGraph] Loaded cached graph from %s (%d files, %d definitions, commit: %.7s)\n",
		path, g.FileCount(), g.DefinitionCount(), g.CommitSHA)
	return &g, nil
}

// BuildFull builds a complete graph by walking the repo and parsing all supported files.
func BuildFull(ctx context.Context, repoPath string) (*Graph, error) {
	startTime := time.Now()
	stats := &BuildStats{Mode: "full_build", CacheStatus: "miss"}

	fmt.Printf("🔨 [CodeGraph] Building full graph for %s...\n", repoPath)

	parser := NewParser()
	g := NewGraph()
	g.CommitSHA = GetCurrentCommitSHA(repoPath)
	g.IndexedAt = time.Now()

	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}

		// Skip hidden dirs and common non-project directories across all supported languages
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		stats.TotalFiles++

		relPath, err := filepath.Rel(repoPath, path)
		if err != nil {
			return nil
		}

		langConfig, ok := GetLanguageForFile(relPath)
		if !ok {
			stats.SkippedFiles++
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		entry, err := parseFileEntry(ctx, parser, content, langConfig)
		if err != nil {
			fmt.Printf("⚠️ [CodeGraph] Failed to parse %s: %v\n", relPath, err)
			return nil
		}

		g.Files[relPath] = *entry
		stats.ParsedFiles++
		stats.Definitions += len(entry.Definitions)
		stats.References += len(entry.References)

		fmt.Printf("  📄 %s — %d defs, %d refs\n", relPath, len(entry.Definitions), len(entry.References))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk repo: %w", err)
	}

	// Build cross-file edges
	g.Edges = buildEdges(g)
	stats.Edges = len(g.Edges)
	stats.Duration = time.Since(startTime)
	stats.Print()

	return g, nil
}

// DeltaUpdate incrementally updates a cached graph by re-parsing only changed files.
func DeltaUpdate(ctx context.Context, g *Graph, repoPath string, changedFiles []string) error {
	startTime := time.Now()
	stats := &BuildStats{
		Mode:        "delta_update",
		CacheStatus: "stale",
		TotalFiles:  g.FileCount(),
		DeltaFiles:  len(changedFiles),
	}

	fmt.Printf("🔄 [CodeGraph] Delta update: re-parsing %d changed files...\n", len(changedFiles))

	parser := NewParser()

	for _, relPath := range changedFiles {
		// 1. Remove old entries
		g.RemoveFile(relPath)

		// 2. Check if file still exists
		fullPath := filepath.Join(repoPath, relPath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			fmt.Printf("  🗑️  %s (deleted or unreadable)\n", relPath)
			continue
		}

		// 3. Re-parse
		langConfig, ok := GetLanguageForFile(relPath)
		if !ok {
			continue
		}

		entry, err := parseFileEntry(ctx, parser, content, langConfig)
		if err != nil {
			fmt.Printf("  ⚠️  %s: parse error: %v\n", relPath, err)
			continue
		}

		g.Files[relPath] = *entry
		stats.Definitions += len(entry.Definitions)
		stats.References += len(entry.References)
		fmt.Printf("  📄 %s — %d defs, %d refs (re-parsed)\n", relPath, len(entry.Definitions), len(entry.References))
	}

	// 4. Rebuild all edges
	g.Edges = buildEdges(g)
	g.CommitSHA = GetCurrentCommitSHA(repoPath)
	g.IndexedAt = time.Now()

	stats.Edges = len(g.Edges)
	stats.Duration = time.Since(startTime)
	stats.Print()

	return nil
}

// parseFileEntry parses a single file and returns its FileEntry.
func parseFileEntry(ctx context.Context, parser *Parser, content []byte, langConfig *LanguageConfig) (*FileEntry, error) {
	root, err := parser.ParseFile(ctx, content, langConfig)
	if err != nil {
		return nil, err
	}

	langName := strings.ToLower(langConfig.Name)

	// Extract definitions with line numbers
	defs, err := parser.ExtractDefinitionEntries(root, content, langName)
	if err != nil {
		defs = nil
	}

	// Extract references
	refs, err := parser.ExtractReferenceDetails(root, content, langName)
	if err != nil {
		refs = nil
	}

	// Filter out builtins from references
	filteredRefs := make([]Reference, 0, len(refs))
	for _, ref := range refs {
		if !isBuiltinSymbol(ref.Symbol) {
			filteredRefs = append(filteredRefs, ref)
		}
	}

	// Compute content hash
	hash := fmt.Sprintf("%x", sha256.Sum256(content))

	return &FileEntry{
		Hash:        hash,
		Language:    langName,
		Definitions: defs,
		References:  filteredRefs,
	}, nil
}

// buildEdges creates cross-file edges by matching references to definitions.
// Uses an automatic frequency-based filter to skip generic symbol names:
// if a symbol is defined in 3+ files, it's too generic to create meaningful edges.
func buildEdges(g *Graph) []Edge {
	// Build definition index: symbol -> file path
	// Also count how many files define each symbol (frequency filter).
	defIndex := make(map[string]string)
	defFrequency := make(map[string]int) // symbol -> number of files defining it
	for path, entry := range g.Files {
		for _, def := range entry.Definitions {
			defFrequency[def.Symbol]++
			if _, exists := defIndex[def.Symbol]; !exists {
				defIndex[def.Symbol] = path
			}
		}
	}

	// Automatic noise filter: skip symbols defined in 3+ files.
	// These are generic names like "String", "Close", "Error", "main"
	// that create meaningless cross-file edges.
	const maxDefFrequency = 3
	var skippedNoisy int

	// Build edges: for each file's references, find the definition file
	var edges []Edge
	seen := make(map[string]struct{})
	for fromPath, entry := range g.Files {
		for _, ref := range entry.References {
			toPath, ok := defIndex[ref.Symbol]
			if !ok || toPath == fromPath {
				continue
			}

			// Skip generic symbols defined in many files
			if defFrequency[ref.Symbol] >= maxDefFrequency {
				skippedNoisy++
				continue
			}

			key := fromPath + "\x00" + toPath + "\x00" + ref.Symbol
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			edges = append(edges, Edge{
				From:     fromPath,
				To:       toPath,
				Symbol:   ref.Symbol,
				Relation: relationForReferenceKind(ref.Kind),
			})
		}
	}

	if skippedNoisy > 0 {
		fmt.Printf("  🔇 [CodeGraph] Filtered %d noisy edges (symbols defined in %d+ files)\n",
			skippedNoisy, maxDefFrequency)
	}
	return edges
}

// GetCurrentCommitSHA returns the current HEAD commit SHA of the repo.
func GetCurrentCommitSHA(repoPath string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
