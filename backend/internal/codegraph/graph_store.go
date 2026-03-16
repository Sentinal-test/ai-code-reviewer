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
	if g.Symbols == nil {
		g.Symbols = make(map[string]Symbol)
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
	g.Version = 4
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
		if isTestFile(relPath) {
			stats.SkippedFiles++
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		entry, err := parseFileEntry(ctx, parser, content, langConfig, relPath)
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

	// Prune references that don't match any definition in the repo
	pruned := pruneUnresolvedReferences(g, repoPath)
	if pruned > 0 {
		fmt.Printf("  🧹 [CodeGraph] Pruned %d unresolved references (stdlib/external)\n", pruned)
	}

	rebuildGraphIndexes(g, repoPath)
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
		if isTestFile(relPath) {
			g.RemoveFile(relPath)
			continue
		}
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

		entry, err := parseFileEntry(ctx, parser, content, langConfig, relPath)
		if err != nil {
			fmt.Printf("  ⚠️  %s: parse error: %v\n", relPath, err)
			continue
		}

		g.Files[relPath] = *entry
		stats.Definitions += len(entry.Definitions)
		stats.References += len(entry.References)
		fmt.Printf("  📄 %s — %d defs, %d refs (re-parsed)\n", relPath, len(entry.Definitions), len(entry.References))
	}

	// 4. Prune unresolved references
	pruned := pruneUnresolvedReferences(g, repoPath)
	if pruned > 0 {
		fmt.Printf("  🧹 [CodeGraph] Pruned %d unresolved references (stdlib/external)\n", pruned)
	}

	// 5. Rebuild all indexes and edges
	rebuildGraphIndexes(g, repoPath)
	g.CommitSHA = GetCurrentCommitSHA(repoPath)
	g.IndexedAt = time.Now()
	g.Version = 4

	stats.Edges = len(g.Edges)
	stats.Duration = time.Since(startTime)
	stats.Print()

	return nil
}

// parseFileEntry parses a single file and returns its FileEntry.
func parseFileEntry(ctx context.Context, parser *Parser, content []byte, langConfig *LanguageConfig, filePath string) (*FileEntry, error) {
	root, err := parser.ParseFile(ctx, content, langConfig)
	if err != nil {
		return nil, err
	}

	langName := strings.ToLower(langConfig.Name)
	namespace := parser.ExtractNamespace(root, content, langName, filePath)

	// Extract definitions with line numbers
	defs, err := parser.ExtractDefinitionEntries(root, content, langName, filePath)
	if err != nil {
		defs = nil
	}

	// Extract references
	refs, err := parser.ExtractReferenceDetailsWithDefinitions(root, content, langName, defs)
	if err != nil {
		refs = nil
	}

	importSpecs := parser.ExtractImportSpecs(content, langName)
	imports := make([]string, 0, len(importSpecs))
	importAliases := make(map[string]string, len(importSpecs))
	for _, spec := range importSpecs {
		imports = append(imports, spec.Path)
		if spec.Alias != "" {
			importAliases[spec.Alias] = spec.Path
		}
	}

	// Filter out builtins before graph-level resolution.
	filteredRefs := make([]Reference, 0, len(refs))
	for _, ref := range refs {
		if isBuiltinSymbol(ref.Symbol) {
			continue
		}
		filteredRefs = append(filteredRefs, ref)
	}

	// Compute content hash
	hash := fmt.Sprintf("%x", sha256.Sum256(content))

	return &FileEntry{
		Hash:          hash,
		Language:      langName,
		Package:       namespace,
		Definitions:   defs,
		References:    filteredRefs,
		Imports:       imports,
		ImportAliases: importAliases,
	}, nil
}

// pruneUnresolvedReferences resolves references against the symbol index and drops
// builtin / external / ambiguous edges before they ever reach graph.json.
func pruneUnresolvedReferences(g *Graph, repoPath string) int {
	lookup := buildSymbolLookup(g)
	goModulePath := loadGoModulePath(repoPath)

	totalPruned := 0
	for path, entry := range g.Files {
		filtered := make([]Reference, 0, len(entry.References))
		changed := false
		for _, ref := range entry.References {
			resolved, keep := resolveReference(g, path, entry, ref, lookup, repoPath, goModulePath)
			if !keep {
				totalPruned++
				changed = true
				continue
			}
			if resolved != ref {
				changed = true
			}
			filtered = append(filtered, resolved)
		}
		if changed || len(filtered) != len(entry.References) {
			entry.References = filtered
			g.Files[path] = entry
		}
	}
	return totalPruned
}

func rebuildGraphIndexes(g *Graph, repoPath string) {
	_ = repoPath
	if g == nil {
		return
	}
	symbols, symbolEdges, fileEdges, packageDeps := buildIndexes(g)
	g.Symbols = symbols
	g.SymbolEdges = symbolEdges
	g.Edges = fileEdges
	g.PackageDeps = packageDeps
}

type symbolLookup struct {
	byQualified      map[string]Symbol
	byName           map[string][]Symbol
	byPackageAndName map[string][]Symbol
	byOwnerAndName   map[string][]Symbol
	byFileAndName    map[string][]Symbol
}

func buildSymbolLookup(g *Graph) symbolLookup {
	lookup := symbolLookup{
		byQualified:      make(map[string]Symbol),
		byName:           make(map[string][]Symbol),
		byPackageAndName: make(map[string][]Symbol),
		byOwnerAndName:   make(map[string][]Symbol),
		byFileAndName:    make(map[string][]Symbol),
	}
	for path, entry := range g.Files {
		for _, def := range entry.Definitions {
			sym := Symbol{
				QualifiedSymbol: def.QualifiedSymbol,
				Symbol:          def.Symbol,
				Package:         def.Package,
				File:            path,
				Owner:           def.Owner,
				Kind:            def.Kind,
				Line:            def.Line,
				EndLine:         def.EndLine,
			}
			lookup.byQualified[sym.QualifiedSymbol] = sym
			lookup.byName[sym.Symbol] = append(lookup.byName[sym.Symbol], sym)
			lookup.byPackageAndName[sym.Package+"\x00"+sym.Symbol] = append(lookup.byPackageAndName[sym.Package+"\x00"+sym.Symbol], sym)
			if sym.Owner != "" {
				key := sym.Package + "\x00" + sym.Owner + "\x00" + sym.Symbol
				lookup.byOwnerAndName[key] = append(lookup.byOwnerAndName[key], sym)
			}
			lookup.byFileAndName[path+"\x00"+sym.Symbol] = append(lookup.byFileAndName[path+"\x00"+sym.Symbol], sym)
		}
	}
	return lookup
}

func resolveReference(g *Graph, filePath string, entry FileEntry, ref Reference, lookup symbolLookup, repoPath, goModulePath string) (Reference, bool) {
	if ref.Symbol == "" {
		return ref, false
	}

	if importPath, ok := entry.ImportAliases[ref.Qualifier]; ok && ref.Qualifier != "" {
		if !isResolvableProjectImport(repoPath, g, filePath, importPath, entry.Language, goModulePath) {
			return ref, false
		}

		namespace := namespaceForImportPath(importPath, entry.Language, goModulePath)
		candidates := lookup.byPackageAndName[namespace+"\x00"+ref.Symbol]
		if len(candidates) == 1 {
			return attachResolvedReference(ref, candidates[0]), true
		}
		// Severe fix: If alias matched but symbol not found, DO NOT fall through to global search.
		// This prevents "uniquely" resolving to a random same-named symbol in another repo.
		return ref, false
	}

	if ref.Qualifier != "" {
		key := entry.Package + "\x00" + ref.Qualifier + "\x00" + ref.Symbol
		if candidates := lookup.byOwnerAndName[key]; len(candidates) == 1 {
			return attachResolvedReference(ref, candidates[0]), true
		}
	}

	if ref.ScopeQualified != "" {
		if scope, ok := lookup.byQualified[ref.ScopeQualified]; ok {
			key := scope.Package + "\x00" + ref.Symbol
			if candidates := lookup.byPackageAndName[key]; len(candidates) == 1 {
				return attachResolvedReference(ref, candidates[0]), true
			}
		}
	}

	if candidates := lookup.byFileAndName[filePath+"\x00"+ref.Symbol]; len(candidates) == 1 {
		return attachResolvedReference(ref, candidates[0]), true
	}
	if candidates := lookup.byPackageAndName[entry.Package+"\x00"+ref.Symbol]; len(candidates) == 1 {
		return attachResolvedReference(ref, candidates[0]), true
	}
	if candidates := lookup.byName[ref.Symbol]; len(candidates) == 1 {
		return attachResolvedReference(ref, candidates[0]), true
	}

	return ref, false
}

func attachResolvedReference(ref Reference, sym Symbol) Reference {
	ref.ResolvedSymbol = sym.Symbol
	ref.ResolvedQualified = sym.QualifiedSymbol
	ref.ResolvedFile = sym.File
	return ref
}

func namespaceForImportPath(importPath, langName, goModulePath string) string {
	importPath = normalizeImportPath(langName, importPath)
	switch langName {
	case "go":
		trimmed := strings.TrimPrefix(importPath, goModulePath)
		trimmed = strings.Trim(trimmed, "/")
		if trimmed == "" {
			return lastSegment(importPath)
		}
		return lastSegment(trimmed)
	case "java":
		return importPath
	default:
		return strings.ReplaceAll(strings.Trim(importPath, "."), "/", ".")
	}
}

func buildIndexes(g *Graph) (map[string]Symbol, []SymbolEdge, []Edge, []PackageDependency) {
	lookup := buildSymbolLookup(g)
	symbols := lookup.byQualified

	fileEdgesSeen := make(map[string]struct{})
	symbolEdgesSeen := make(map[string]struct{})
	packageDepsSeen := make(map[string]struct{})

	var symbolEdges []SymbolEdge
	var fileEdges []Edge
	var packageDeps []PackageDependency

	for fromPath, entry := range g.Files {
		for _, ref := range entry.References {
			if ref.ResolvedQualified == "" {
				continue
			}
			target, ok := lookup.byQualified[ref.ResolvedQualified]
			if !ok {
				continue
			}

			// Severe fix: If ref is top-level (no ScopeQualified), use file/package as source.
			// This ensures fileEdges and packageDeps are created for non-encapsulated code.
			fromQualified := ref.ScopeQualified
			var source Symbol
			if fromQualified == "" {
				source = Symbol{
					File:    fromPath,
					Package: entry.Package,
				}
			} else {
				source, ok = lookup.byQualified[fromQualified]
				if !ok {
					continue
				}
			}

			if fromQualified != "" {
				symbolKey := fromQualified + "\x00" + target.QualifiedSymbol + "\x00" + ref.Kind
				if _, exists := symbolEdgesSeen[symbolKey]; !exists {
					symbolEdgesSeen[symbolKey] = struct{}{}
					symbolEdges = append(symbolEdges, SymbolEdge{
						From:     fromQualified,
						To:       target.QualifiedSymbol,
						Symbol:   ref.Symbol,
						FromFile: fromPath,
						ToFile:   target.File,
						Relation: relationForReferenceKind(ref.Kind),
					})
				}
			}

			if source.File != target.File {
				fileKey := source.File + "\x00" + target.File + "\x00" + ref.Symbol
				if _, exists := fileEdgesSeen[fileKey]; !exists {
					fileEdgesSeen[fileKey] = struct{}{}
					fileEdges = append(fileEdges, Edge{
						From:     source.File,
						To:       target.File,
						Symbol:   ref.Symbol,
						Relation: relationForReferenceKind(ref.Kind),
					})
				}
			}

			if source.Package != "" && target.Package != "" && source.Package != target.Package {
				pkgKey := source.Package + "\x00" + target.Package
				if _, exists := packageDepsSeen[pkgKey]; !exists {
					packageDepsSeen[pkgKey] = struct{}{}
					packageDeps = append(packageDeps, PackageDependency{
						From: source.Package,
						To:   target.Package,
						Via:  ref.Symbol,
					})
				}
			}
		}
	}

	return symbols, symbolEdges, fileEdges, packageDeps
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
