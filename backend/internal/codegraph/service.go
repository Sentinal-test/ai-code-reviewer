package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MaxDepContextChars keeps dependency summaries compact for the LLM context window.
const MaxDepContextChars = 12000

// MaxSymbolsPerSummary limits per-file symbol lists to avoid verbose summaries.
const MaxSymbolsPerSummary = 8

// MaxSnippetsPerFile caps captured snippets used only for deterministic data-flow inference.
const MaxSnippetsPerFile = 6

// MaxBehaviorSignals caps inferred behavior hints per summary.
const MaxBehaviorSignals = 4

// MaxGraphEdgeSymbols limits symbol examples shown per graph edge.
const MaxGraphEdgeSymbols = 4

// MaxDataFlowLines limits emitted data-flow chains.
const MaxDataFlowLines = 12

// Service is the main entry point for code graph operations.
type Service struct {
	RepoPath string
	Parser   *Parser
	Scanner  *Scanner
	Graph    *Graph // cached graph for fast lookups, nil if no cache
}

// NewService creates a new code graph service for the given repo.
func NewService(repoPath string) *Service {
	return &Service{
		RepoPath: repoPath,
		Parser:   NewParser(),
		Scanner:  NewScanner(repoPath),
	}
}

// SetGraph injects a previously loaded graph for fast symbol lookups.
func (s *Service) SetGraph(g *Graph) {
	s.Graph = g
}

// GetContext analyzes changed files and returns compact summaries:
// - per dependency file behavior summary
// - a context-graph relationship summary with data flow
func (s *Service) GetContext(ctx context.Context, changedFiles map[string]string, diff string) (map[string]string, error) {
	startTime := time.Now()
	stats := &BuildStats{Mode: "analysis"}

	fmt.Println("🔍 [CodeGraph] Starting deterministic context analysis...")

	if s.Graph != nil && diff != "" {
		if runtimeResult := s.getRuntimeSliceContext(ctx, changedFiles, diff); len(runtimeResult) > 0 {
			stats.TotalFiles = len(changedFiles)
			stats.Duration = time.Since(startTime)
			stats.Print()
			fmt.Printf("✅ [CodeGraph] Runtime slice complete. Produced %d context entries.\n", len(runtimeResult))
			return runtimeResult, nil
		}
		fmt.Println("⚠️ [CodeGraph] Runtime slice unavailable; falling back to scan-based dependency discovery.")
	}

	referencedSymbols := make(map[string]string) // symbol -> lang
	refsByChangedFile := make(map[string][]Reference)

	// 1. Extract references from changed files
	fmt.Println("📝 [CodeGraph] Input: Analyzing the following changed files:")
	for _, path := range sortedMapKeys(changedFiles) {
		fmt.Printf("  - %s\n", path)
	}

	for path, content := range changedFiles {
		langConfig, ok := GetLanguageForFile(path)
		if !ok {
			stats.SkippedFiles++
			continue
		}

		root, err := s.Parser.ParseFile(ctx, []byte(content), langConfig)
		if err != nil {
			fmt.Printf("⚠️ [CodeGraph] Failed to parse %s: %v\n", path, err)
			continue
		}
		stats.ParsedFiles++

		referenceDetails, err := s.Parser.ExtractReferenceDetails(root, []byte(content), strings.ToLower(langConfig.Name))
		if err != nil {
			fmt.Printf("⚠️ [CodeGraph] Failed to extract refs from %s: %v\n", path, err)
			continue
		}

		// Filter out builtins before adding
		filtered := 0
		for _, ref := range referenceDetails {
			if isBuiltinSymbol(ref.Symbol) {
				filtered++
				continue
			}

			referencedSymbols[ref.Symbol] = strings.ToLower(langConfig.Name)
			refsByChangedFile[path] = addReferenceUnique(refsByChangedFile[path], ref)
			stats.References++
		}

		if len(referenceDetails) > 0 {
			fmt.Printf("   -> Found %d references in %s (%d builtin filtered)\n", len(referenceDetails)-filtered, path, filtered)
		}
	}

	fmt.Printf("🔍 [CodeGraph] Searching for %d unique project symbols...\n", len(referencedSymbols))
	if len(referencedSymbols) > 0 {
		symList := make([]string, 0, len(referencedSymbols))
		for s := range referencedSymbols {
			symList = append(symList, s)
		}
		fmt.Printf("   Symbols: %v\n", symList)
	}

	// 2. Find definitions and extract only enough detail to build summaries
	foundDeps := make(map[string]*dependencyInfo)
	symbolToDefPath := make(map[string]string) // symbol -> dependency file path
	var mu sync.Mutex
	var wg sync.WaitGroup

	semaphore := make(chan struct{}, 10)

	for symbol, lang := range referencedSymbols {
		wg.Add(1)
		go func(sym, l string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			langConfig, _ := SupportedLanguages[l]
			candidates, err := s.Scanner.FindCandidates(ctx, sym, langConfig.Extensions)
			if err != nil {
				return
			}

			for _, candidatePath := range candidates {
				mu.Lock()
				if _, exists := changedFiles[candidatePath]; exists {
					mu.Unlock()
					continue
				}
				mu.Unlock()

				// Read file content
				fullPath := filepath.Join(s.RepoPath, candidatePath)
				contentBytes, err := os.ReadFile(fullPath)
				if err != nil {
					continue
				}

				candidateLang, ok := GetLanguageForFile(candidatePath)
				if !ok {
					continue
				}

				root, err := s.Parser.ParseFile(ctx, contentBytes, candidateLang)
				if err != nil {
					continue
				}

				// Extract ONLY the matching definition snippet, not the full file
				snippet, err := s.Parser.ExtractDefinitionSnippet(root, contentBytes, strings.ToLower(candidateLang.Name), sym)
				if err != nil || snippet == "" {
					continue
				}

				mu.Lock()
				if _, alreadyMapped := symbolToDefPath[sym]; !alreadyMapped {
					symbolToDefPath[sym] = candidatePath
				}

				dep := foundDeps[candidatePath]
				if dep == nil {
					dep = &dependencyInfo{
						Symbols:     make(map[string]struct{}),
						Definitions: make(map[string]struct{}),
					}
					foundDeps[candidatePath] = dep
				}
				dep.Symbols[sym] = struct{}{}
				if len(dep.Snippets) < MaxSnippetsPerFile && !containsString(dep.Snippets, snippet) {
					dep.Snippets = append(dep.Snippets, snippet)
				}

				preview := snippet
				if len(preview) > 60 {
					preview = preview[:57] + "..."
				}
				preview = strings.ReplaceAll(preview, "\n", " ")
				fmt.Printf("✅ [CodeGraph] Found definition for '%s' in %s\n", sym, candidatePath)
				fmt.Printf("   Snippet: %s\n", preview)
				stats.Definitions++
				mu.Unlock()

				// A single concrete definition is enough for deterministic context.
				break
			}
		}(symbol, lang)
	}

	wg.Wait()

	// 3. Parse selected dependency files once to extract definitions and their own references.
	definitionIndex := make(map[string]string)
	for symbol, path := range symbolToDefPath {
		definitionIndex[symbol] = path
	}
	for _, path := range sortedDependencyKeys(foundDeps) {
		s.enrichDependencyMetadata(ctx, path, foundDeps[path], definitionIndex)
	}

	result := make(map[string]string)
	totalContextSize := 0

	for _, path := range sortedDependencyKeys(foundDeps) {
		dep := foundDeps[path]
		summary := buildDependencySummary(path, dep, definitionIndex)
		if summary == "" {
			continue
		}
		added := len(summary)
		if totalContextSize+added > MaxDepContextChars {
			continue
		}
		result[path] = summary
		totalContextSize += added
	}

	contextGraph := buildContextGraphSummary(changedFiles, refsByChangedFile, foundDeps, definitionIndex)
	if contextGraph != "" {
		if totalContextSize+len(contextGraph) <= MaxDepContextChars {
			result["_codegraph/context_graph"] = contextGraph
			totalContextSize += len(contextGraph)
		}
	}

	// Count edges for stats
	for _, v := range result {
		if strings.Contains(v, "CONTEXT GRAPH") {
			for _, line := range strings.Split(v, "\n") {
				if strings.Contains(line, "-->") {
					stats.Edges++
				}
			}
		}
	}

	stats.TotalFiles = len(changedFiles)
	stats.Duration = time.Since(startTime)
	stats.Print()

	fmt.Printf("✅ [CodeGraph] Analysis complete. Found summaries for %d files (%d chars of context).\n", len(result), totalContextSize)
	if len(result) > 0 {
		fmt.Println("📂 [CodeGraph] Output: Added the following files to context:")
		for _, path := range sortedMapKeys(result) {
			fmt.Printf("  + %s\n", path)
		}
	}
	return result, nil
}

func (s *Service) getRuntimeSliceContext(ctx context.Context, changedFiles map[string]string, diff string) map[string]string {
	_ = ctx
	if s.Graph == nil {
		return nil
	}

	view := s.Graph.BuildRuntimeGraphView(diff, changedFiles, 2)
	if view == nil || len(view.Symbols) == 0 {
		return nil
	}

	result := make(map[string]string)
	result["_codegraph/runtime_slice"] = FormatRuntimeGraphView(view)

	depsByFile := make(map[string]*dependencyInfo)
	for _, sym := range view.Symbols {
		if _, changed := changedFiles[sym.File]; changed {
			continue
		}

		dep := depsByFile[sym.File]
		if dep == nil {
			dep = &dependencyInfo{
				Symbols:     make(map[string]struct{}),
				Definitions: make(map[string]struct{}),
			}
			depsByFile[sym.File] = dep
		}
		dep.Symbols[sym.Symbol] = struct{}{}
		dep.Definitions[sym.QualifiedSymbol] = struct{}{}

		snippet := s.symbolSnippet(sym)
		if snippet != "" && len(dep.Snippets) < MaxSnippetsPerFile && !containsString(dep.Snippets, snippet) {
			dep.Snippets = append(dep.Snippets, snippet)
		}
	}

	for path, dep := range depsByFile {
		if entry, ok := s.Graph.Files[path]; ok {
			dep.References = append(dep.References, entry.References...)
		}
	}

	definitionIndex := make(map[string]string)
	for _, sym := range view.Symbols {
		definitionIndex[sym.Symbol] = sym.File
		if sym.QualifiedSymbol != "" {
			definitionIndex[sym.QualifiedSymbol] = sym.File
		}
	}

	totalContextSize := len(result["_codegraph/runtime_slice"])
	for _, path := range sortedDependencyKeys(depsByFile) {
		summary := buildDependencySummary(path, depsByFile[path], definitionIndex)
		if summary == "" {
			continue
		}
		if totalContextSize+len(summary) > MaxDepContextChars {
			break
		}
		result[path] = summary
		totalContextSize += len(summary)
	}

	return result
}

func (s *Service) symbolSnippet(sym Symbol) string {
	if s.RepoPath == "" || sym.File == "" || sym.Line <= 0 {
		return ""
	}
	contentBytes, err := os.ReadFile(filepath.Join(s.RepoPath, sym.File))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(contentBytes), "\n")
	start := sym.Line - 1
	end := sym.EndLine
	if start < 0 {
		start = 0
	}
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start >= end || start >= len(lines) {
		return ""
	}
	snippet := strings.Join(lines[start:end], "\n")
	if len(snippet) > 3000 {
		snippet = snippet[:3000] + "\n// ... (truncated)"
	}
	return snippet
}

// enrichDependencyMetadata extracts definitions and references from a dependency file.
func (s *Service) enrichDependencyMetadata(ctx context.Context, path string, dep *dependencyInfo, definitionIndex map[string]string) {
	if dep == nil {
		return
	}
	if dep.Definitions == nil {
		dep.Definitions = make(map[string]struct{})
	}

	fullPath := filepath.Join(s.RepoPath, path)
	contentBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return
	}

	langConfig, ok := GetLanguageForFile(path)
	if !ok {
		return
	}

	root, err := s.Parser.ParseFile(ctx, contentBytes, langConfig)
	if err != nil {
		return
	}

	definitions, err := s.Parser.ExtractDefinitions(root, contentBytes, strings.ToLower(langConfig.Name))
	if err == nil {
		for _, def := range definitions {
			if def == "" || isBuiltinSymbol(def) {
				continue
			}
			dep.Definitions[def] = struct{}{}
			if _, exists := definitionIndex[def]; !exists {
				definitionIndex[def] = path
			}
		}
	}

	references, err := s.Parser.ExtractReferenceDetails(root, contentBytes, strings.ToLower(langConfig.Name))
	if err == nil {
		for _, ref := range references {
			if ref.Symbol == "" || isBuiltinSymbol(ref.Symbol) {
				continue
			}
			dep.References = addReferenceUnique(dep.References, ref)
		}
	}
}
