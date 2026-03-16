package llm

import (
	"code-review/backend/internal/agents"
	"code-review/backend/internal/codegraph"
	"code-review/backend/internal/remotefetch"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// AgentToolDeclarations returns the tool declarations for agent tool calls.
func AgentToolDeclarations() []ToolDeclaration {
	return []ToolDeclaration{
		{
			Name:        "get_symbol_definition",
			Description: "Get the source code definition of a symbol (function, type, struct, class, interface). Use this when you see a symbol in the diff that you need to understand.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"symbol": map[string]interface{}{
						"type":        "string",
						"description": "The symbol name to look up (e.g. 'ProcessPR', 'AuthService')",
					},
				},
				"required": []string{"symbol"},
			},
		},
		{
			Name:        "get_file_content",
			Description: "Get the full content of a file from the repository. Use this to inspect files not included in the diff.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative path to the file (e.g. 'internal/llm/llm.go')",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "get_callers",
			Description: "Find all files and locations that reference or call a given symbol. Use this to check if a function is used elsewhere.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"symbol": map[string]interface{}{
						"type":        "string",
						"description": "The symbol name to find callers for",
					},
				},
				"required": []string{"symbol"},
			},
		},
		{
			Name:        "search_codebase",
			Description: "Search the codebase for a text pattern using git grep. Returns matching lines with file paths.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The text pattern to search for",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "list_cross_repo_matches",
			Description: "List all cross-repository matches available for this PR context. Use this to discover which other repositories share code with the current PR.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "resolve_repo_symbol",
			Description: "Find which file defines a symbol inside a remote repository. Supports both plain and qualified symbols.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo_full_name": map[string]interface{}{
						"type":        "string",
						"description": "The target repository. Must be retrieved from `list_cross_repo_matches`.",
					},
					"symbol": map[string]interface{}{
						"type":        "string",
						"description": "The symbol to find (e.g. 'AuthService').",
					},
				},
				"required": []string{"repo_full_name", "symbol"},
			},
		},
		{
			Name:        "get_repo_callers",
			Description: "Find call-sites inside a remote repository that reference a symbol. Optionally constrain the match to a specific imported package path.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo_full_name": map[string]interface{}{
						"type":        "string",
						"description": "The target repository. Must be retrieved from `list_cross_repo_matches`.",
					},
					"symbol": map[string]interface{}{
						"type":        "string",
						"description": "The symbol to find callers for.",
					},
					"import_path": map[string]interface{}{
						"type":        "string",
						"description": "Optional package/module import path to restrict the search to a specific external dependency.",
					},
				},
				"required": []string{"repo_full_name", "symbol"},
			},
		},
		{
			Name:        "fetch_repo_snippet",
			Description: "Fetch a specific snippet of code from a remote repository.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo_full_name": map[string]interface{}{
						"type":        "string",
						"description": "The target repository.",
					},
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file inside the remote repo.",
					},
					"start_line": map[string]interface{}{
						"type":        "integer",
						"description": "The starting line number (1-indexed).",
					},
					"end_line": map[string]interface{}{
						"type":        "integer",
						"description": "The ending line number (1-indexed).",
					},
				},
				"required": []string{"repo_full_name", "file_path", "start_line", "end_line"},
			},
		},
		{
			Name:        "search_repo_graph",
			Description: "Search the runtime graph of a remote repository for definitions, callers, imports, and package dependencies.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo_full_name": map[string]interface{}{
						"type":        "string",
						"description": "The target repository.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The string to match against exported definitions or imports.",
					},
				},
				"required": []string{"repo_full_name", "query"},
			},
		},
	}
}

// ToolExecutor handles executing tool calls locally using the code graph and filesystem.
type ToolExecutor struct {
	RepoPath string
	Graph    *codegraph.Graph

	// Multi-repo extensions
	MatchSummary string
	RemoteGraphs map[string]*codegraph.RemoteRepoGraph
	RemoteFetch  *remotefetch.Fetcher
}

// NewToolExecutor creates a tool executor for the given repo.
func NewToolExecutor(repoPath string, graph *codegraph.Graph) *ToolExecutor {
	return &ToolExecutor{
		RepoPath: repoPath,
		Graph:    graph,
	}
}

// WithMultiRepo attaches the multi-repo context to the ToolExecutor.
func (te *ToolExecutor) WithMultiRepo(matchSummary string, remote map[string]*codegraph.RemoteRepoGraph, fetch *remotefetch.Fetcher) {
	te.MatchSummary = matchSummary
	te.RemoteGraphs = remote
	te.RemoteFetch = fetch
}

// Execute runs a tool call and returns the result.
func (te *ToolExecutor) Execute(ctx context.Context, call agents.ToolCallRequest) agents.ToolCallResponse {
	switch call.Name {
	case "get_symbol_definition":
		return te.getSymbolDefinition(call.Args)
	case "get_file_content":
		return te.getFileContent(call.Args)
	case "get_callers":
		return te.getCallers(call.Args)
	case "search_codebase":
		return te.searchCodebase(ctx, call.Args)
	case "list_cross_repo_matches":
		return te.listCrossRepoMatches()
	case "resolve_repo_symbol":
		return te.resolveRepoSymbol(call.Args)
	case "get_repo_callers":
		return te.getRepoCallers(call.Args)
	case "fetch_repo_snippet":
		return te.fetchRepoSnippet(ctx, call.Args)
	case "search_repo_graph":
		return te.searchRepoGraph(call.Args)
	default:
		return agents.ToolCallResponse{
			Name:    call.Name,
			Content: fmt.Sprintf("Unknown tool: %s", call.Name),
		}
	}
}

func (te *ToolExecutor) getSymbolDefinition(args map[string]interface{}) agents.ToolCallResponse {
	symbol, _ := args["symbol"].(string)
	if symbol == "" {
		return agents.ToolCallResponse{Name: "get_symbol_definition", Content: "Error: symbol parameter is required"}
	}

	if te.Graph == nil {
		return agents.ToolCallResponse{Name: "get_symbol_definition", Content: "Error: code graph not available"}
	}

	path, def := te.Graph.FindDefinition(symbol)
	if path == "AMBIGUOUS" {
		candidates := te.Graph.FindDefinitions(symbol)
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Symbol '%s' is ambiguous. Please use one of the qualified names below:\n", symbol))
		for candPath, candDef := range candidates {
			b.WriteString(fmt.Sprintf("  - %s (File: %s, Line: %d, Kind: %s)\n", candDef.QualifiedSymbol, candPath, candDef.Line, candDef.Kind))
		}
		return agents.ToolCallResponse{Name: "get_symbol_definition", Content: b.String()}
	}

	if def == nil {
		return agents.ToolCallResponse{
			Name:    "get_symbol_definition",
			Content: fmt.Sprintf("Symbol '%s' not found. Use search_codebase if you are unsure of the exact name.", symbol),
		}
	}

	result := fmt.Sprintf("Symbol: %s\nQualified: %s\nFile: %s\nKind: %s\nLine: %d\n",
		def.Symbol, def.QualifiedSymbol, path, def.Kind, def.Line)

	// Read the actual source from the file using line ranges
	if def.EndLine > 0 && te.RepoPath != "" {
		fullPath := filepath.Join(te.RepoPath, path)
		if content, err := os.ReadFile(fullPath); err == nil {
			lines := strings.Split(string(content), "\n")
			startIdx := def.Line - 1
			endIdx := def.EndLine
			if startIdx < 0 {
				startIdx = 0
			}
			if endIdx > len(lines) {
				endIdx = len(lines)
			}
			snippet := strings.Join(lines[startIdx:endIdx], "\n")
			const maxSnippet = 5000 // Increased from 3000
			if len(snippet) > maxSnippet {
				snippet = snippet[:maxSnippet] + "\n// ... (truncated)"
			}
			result += fmt.Sprintf("\nSource:\n%s", snippet)
		}
	}

	return agents.ToolCallResponse{Name: "get_symbol_definition", Content: result}
}

func (te *ToolExecutor) getFileContent(args map[string]interface{}) agents.ToolCallResponse {
	path, _ := args["path"].(string)
	if path == "" {
		return agents.ToolCallResponse{Name: "get_file_content", Content: "Error: path parameter is required"}
	}

	fullPath := filepath.Join(te.RepoPath, path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return agents.ToolCallResponse{
			Name:    "get_file_content",
			Content: fmt.Sprintf("Error reading file '%s': %v", path, err),
		}
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	// Support windowed reading
	startLineF, hasStart := args["start_line"].(float64)
	endLineF, hasEnd := args["end_line"].(float64)

	if hasStart || hasEnd {
		start := 1
		if hasStart {
			start = int(startLineF)
		}
		end := len(lines)
		if hasEnd {
			end = int(endLineF)
		}

		if start < 1 {
			start = 1
		}
		if end > len(lines) {
			end = len(lines)
		}
		if start > end {
			return agents.ToolCallResponse{Name: "get_file_content", Content: "Error: start_line cannot be greater than end_line"}
		}

		content = strings.Join(lines[start-1:end], "\n")
		return agents.ToolCallResponse{Name: "get_file_content", Content: content}
	}

	// Cap full file content to prevent massive responses
	const maxChars = 40000 // Increased from 20000
	if len(content) > maxChars {
		content = content[:maxChars] + "\n... (truncated at 40K chars. Use start_line/end_line to read specific sections.)"
	}

	return agents.ToolCallResponse{Name: "get_file_content", Content: content}
}

func (te *ToolExecutor) getCallers(args map[string]interface{}) agents.ToolCallResponse {
	symbol, _ := args["symbol"].(string)
	if symbol == "" {
		return agents.ToolCallResponse{Name: "get_callers", Content: "Error: symbol parameter is required"}
	}

	if te.Graph == nil {
		return agents.ToolCallResponse{Name: "get_callers", Content: "Error: code graph not available"}
	}

	// USE the inverted index for O(1) lookups if available
	var callers []string
	if te.Graph.InvertedReferences != nil {
		for _, loc := range te.Graph.InvertedReferences[symbol] {
			callers = append(callers, fmt.Sprintf("  %s:%d -> %s (kind: %s)", loc.File, loc.Line, loc.ScopeQualified, loc.Kind))
		}
	} else {
		// Fallback for non-indexed graph
		for path, entry := range te.Graph.Files {
			for _, ref := range entry.References {
				if ref.ResolvedQualified == symbol || ref.Symbol == symbol {
					callers = append(callers, fmt.Sprintf("  %s:%d -> %s (kind: %s)", path, ref.Line, ref.ScopeQualified, ref.Kind))
				}
			}
		}
	}

	if len(callers) == 0 {
		return agents.ToolCallResponse{
			Name:    "get_callers",
			Content: fmt.Sprintf("No callers/references found for symbol '%s'.", symbol),
		}
	}

	sort.Strings(callers)
	// Deduplicate if needed (same location might be indexed twice by symbol and qualified symbol)
	uniqueCallers := make([]string, 0, len(callers))
	last := ""
	for _, c := range callers {
		if c != last {
			uniqueCallers = append(uniqueCallers, c)
			last = c
		}
	}

	return agents.ToolCallResponse{
		Name:    "get_callers",
		Content: fmt.Sprintf("Symbol '%s' is referenced at %d call-sites:\n%s", symbol, len(uniqueCallers), strings.Join(uniqueCallers, "\n")),
	}
}

func (te *ToolExecutor) searchCodebase(ctx context.Context, args map[string]interface{}) agents.ToolCallResponse {
	query, _ := args["query"].(string)
	if query == "" {
		return agents.ToolCallResponse{Name: "search_codebase", Content: "Error: query parameter is required"}
	}

	cmd := exec.CommandContext(ctx, "git", "grep", "-n", "-I", "--max-count=5", query)
	cmd.Dir = te.RepoPath

	out, _ := cmd.Output()
	result := strings.TrimSpace(string(out))
	if result == "" {
		return agents.ToolCallResponse{
			Name:    "search_codebase",
			Content: fmt.Sprintf("No matches found for '%s'.", query),
		}
	}

	// Cap results
	lines := strings.Split(result, "\n")
	if len(lines) > 30 {
		lines = lines[:30]
		lines = append(lines, fmt.Sprintf("... (+%d more matches)", len(strings.Split(result, "\n"))-30))
	}

	return agents.ToolCallResponse{
		Name:    "search_codebase",
		Content: strings.Join(lines, "\n"),
	}
}

// -- Multi-Repo Tools --

func (te *ToolExecutor) listCrossRepoMatches() agents.ToolCallResponse {
	if te.MatchSummary == "" {
		return agents.ToolCallResponse{
			Name:    "list_cross_repo_matches",
			Content: "No cross-repository matches are available for this PR.",
		}
	}
	return agents.ToolCallResponse{
		Name:    "list_cross_repo_matches",
		Content: te.MatchSummary,
	}
}

func (te *ToolExecutor) resolveRepoSymbol(args map[string]interface{}) agents.ToolCallResponse {
	repoFullName, _ := args["repo_full_name"].(string)
	symbol, _ := args["symbol"].(string)

	if repoFullName == "" || symbol == "" {
		return agents.ToolCallResponse{Name: "resolve_repo_symbol", Content: "Error: repo_full_name and symbol are required"}
	}

	graph, err := te.lookupRemoteGraph(repoFullName)
	if err != nil {
		return agents.ToolCallResponse{Name: "resolve_repo_symbol", Content: err.Error()}
	}

	if sym, ok := graph.Symbols[symbol]; ok {
		return agents.ToolCallResponse{
			Name: "resolve_repo_symbol",
			Content: fmt.Sprintf(
				"Symbol: %s\nQualified: %s\nFile: %s\nPackage: %s\nKind: %s\nLine: %d",
				sym.Symbol,
				sym.QualifiedSymbol,
				sym.File,
				sym.Package,
				sym.Kind,
				sym.Line,
			),
		}
	}

	var found []codegraph.Symbol
	for _, sym := range graph.Symbols {
		if sym.Symbol == symbol || sym.QualifiedSymbol == symbol {
			found = append(found, sym)
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].QualifiedSymbol == found[j].QualifiedSymbol {
			return found[i].File < found[j].File
		}
		return found[i].QualifiedSymbol < found[j].QualifiedSymbol
	})

	if len(found) == 0 {
		return agents.ToolCallResponse{Name: "resolve_repo_symbol", Content: fmt.Sprintf("Symbol %s not found in remote repository %s", symbol, repoFullName)}
	}

	if len(found) > 1 {
		lines := make([]string, 0, len(found))
		for _, sym := range found {
			lines = append(lines, fmt.Sprintf("  - %s (%s:%d, kind: %s)", sym.QualifiedSymbol, sym.File, sym.Line, sym.Kind))
		}
		return agents.ToolCallResponse{
			Name:    "resolve_repo_symbol",
			Content: fmt.Sprintf("Symbol %s is ambiguous in %s. Use one of:\n%s", symbol, repoFullName, strings.Join(lines, "\n")),
		}
	}

	sym := found[0]
	return agents.ToolCallResponse{
		Name: "resolve_repo_symbol",
		Content: fmt.Sprintf(
			"Symbol: %s\nQualified: %s\nFile: %s\nPackage: %s\nKind: %s\nLine: %d",
			sym.Symbol,
			sym.QualifiedSymbol,
			sym.File,
			sym.Package,
			sym.Kind,
			sym.Line,
		),
	}
}

func (te *ToolExecutor) getRepoCallers(args map[string]interface{}) agents.ToolCallResponse {
	repoFullName, _ := args["repo_full_name"].(string)
	symbol, _ := args["symbol"].(string)
	importPath, _ := args["import_path"].(string)

	if repoFullName == "" || symbol == "" {
		return agents.ToolCallResponse{Name: "get_repo_callers", Content: "Error: repo_full_name and symbol are required"}
	}

	graph, err := te.lookupRemoteGraph(repoFullName)
	if err != nil {
		return agents.ToolCallResponse{Name: "get_repo_callers", Content: err.Error()}
	}

	var callers []string
	if importPath == "" && graph.InvertedReferences != nil {
		for _, loc := range graph.InvertedReferences[symbol] {
			callers = append(callers, fmt.Sprintf("  %s:%d -> %s (kind: %s)", loc.File, loc.Line, loc.ScopeQualified, loc.Kind))
		}
		if sym, ok := graph.Symbols[symbol]; ok {
			for _, loc := range graph.InvertedReferences[sym.QualifiedSymbol] {
				callers = append(callers, fmt.Sprintf("  %s:%d -> %s (kind: %s)", loc.File, loc.Line, loc.ScopeQualified, loc.Kind))
			}
		}
	}

	if importPath != "" || len(callers) == 0 {
		for path, entry := range graph.Files {
			for _, ref := range entry.References {
				if ref.Symbol != symbol {
					continue
				}
				if importPath != "" && !matchesRemoteImportReference(entry, ref, importPath) {
					continue
				}
				callers = append(callers, fmt.Sprintf("  %s:%d -> %s (kind: %s)", path, ref.Line, ref.ScopeQualified, ref.Kind))
			}
		}
	}

	sort.Strings(callers)
	callers = dedupeStrings(callers)

	if len(callers) == 0 {
		scope := ""
		if importPath != "" {
			scope = " via " + importPath
		}
		return agents.ToolCallResponse{
			Name:    "get_repo_callers",
			Content: fmt.Sprintf("No callers/references found for symbol '%s' in %s%s.", symbol, repoFullName, scope),
		}
	}

	return agents.ToolCallResponse{
		Name:    "get_repo_callers",
		Content: fmt.Sprintf("Symbol '%s' is referenced at %d call-sites in %s:\n%s", symbol, len(callers), repoFullName, strings.Join(callers, "\n")),
	}
}

func (te *ToolExecutor) fetchRepoSnippet(ctx context.Context, args map[string]interface{}) agents.ToolCallResponse {
	repoFullName, _ := args["repo_full_name"].(string)
	filePath, _ := args["file_path"].(string)
	startLineF, _ := args["start_line"].(float64) // JSON unmarshals ints as float64
	endLineF, _ := args["end_line"].(float64)

	if repoFullName == "" || filePath == "" {
		return agents.ToolCallResponse{Name: "fetch_repo_snippet", Content: "Error: repo_full_name and file_path are required"}
	}

	if te.RemoteFetch == nil {
		return agents.ToolCallResponse{Name: "fetch_repo_snippet", Content: "Error: remote fetcher not initialized"}
	}

	snippet, err := te.RemoteFetch.FetchSnippet(ctx, repoFullName, filePath, int(startLineF), int(endLineF))
	if err != nil {
		return agents.ToolCallResponse{Name: "fetch_repo_snippet", Content: fmt.Sprintf("Error fetching snippet from %s: %v", repoFullName, err)}
	}

	return agents.ToolCallResponse{
		Name:    "fetch_repo_snippet",
		Content: snippet,
	}
}

func (te *ToolExecutor) searchRepoGraph(args map[string]interface{}) agents.ToolCallResponse {
	repoFullName, _ := args["repo_full_name"].(string)
	query, _ := args["query"].(string)

	if repoFullName == "" || query == "" {
		return agents.ToolCallResponse{Name: "search_repo_graph", Content: "Error: repo_full_name and query are required"}
	}

	graph, err := te.lookupRemoteGraph(repoFullName)
	if err != nil {
		return agents.ToolCallResponse{Name: "search_repo_graph", Content: err.Error()}
	}

	queryLower := strings.ToLower(query)
	var matches []string

	for _, sym := range graph.Symbols {
		if strings.Contains(strings.ToLower(sym.Symbol), queryLower) || strings.Contains(strings.ToLower(sym.QualifiedSymbol), queryLower) {
			matches = append(matches, fmt.Sprintf("[DEF] %s at %s:%d (%s)", sym.QualifiedSymbol, sym.File, sym.Line, sym.Kind))
		}
	}
	for _, edge := range graph.SymbolEdges {
		if strings.Contains(strings.ToLower(edge.From), queryLower) || strings.Contains(strings.ToLower(edge.To), queryLower) || strings.Contains(strings.ToLower(edge.Symbol), queryLower) {
			matches = append(matches, fmt.Sprintf("[EDGE] %s -> %s (%s via %s)", edge.From, edge.To, edge.Relation, edge.Symbol))
		}
	}
	for _, dep := range graph.PackageDeps {
		if strings.Contains(strings.ToLower(dep.From), queryLower) || strings.Contains(strings.ToLower(dep.To), queryLower) || strings.Contains(strings.ToLower(dep.Via), queryLower) {
			matches = append(matches, fmt.Sprintf("[PKG] %s -> %s (via %s)", dep.From, dep.To, dep.Via))
		}
	}
	for path, entry := range graph.Files {
		for _, imp := range entry.Imports {
			if strings.Contains(strings.ToLower(imp), queryLower) {
				matches = append(matches, fmt.Sprintf("[IMPORT] %s in %s", imp, path))
			}
		}
		for _, ref := range entry.References {
			if strings.Contains(strings.ToLower(ref.Symbol), queryLower) {
				label := ref.Symbol
				if importPath, ok := entry.ImportAliases[ref.Qualifier]; ok && ref.Qualifier != "" {
					label = fmt.Sprintf("%s via %s", ref.Symbol, importPath)
				} else if importPath, ok := entry.ImportAliases[ref.Symbol]; ok {
					label = fmt.Sprintf("%s via %s", ref.Symbol, importPath)
				}
				matches = append(matches, fmt.Sprintf("[REF] %s:%d uses %s", path, ref.Line, label))
			}
		}
	}

	if len(matches) == 0 {
		return agents.ToolCallResponse{Name: "search_repo_graph", Content: fmt.Sprintf("No graph nodes matched '%s' in %s", query, repoFullName)}
	}

	sort.Strings(matches)
	matches = dedupeStrings(matches)

	// Cap at 50 results
	if len(matches) > 50 {
		matches = matches[:50]
		matches = append(matches, "... (truncated max 50 hits)")
	}

	return agents.ToolCallResponse{
		Name:    "search_repo_graph",
		Content: strings.Join(matches, "\n"),
	}
}

func (te *ToolExecutor) lookupRemoteGraph(repoFullName string) (*codegraph.RemoteRepoGraph, error) {
	if te.RemoteGraphs == nil {
		return nil, fmt.Errorf("Error: remote graphs not initialized")
	}
	graph, ok := te.RemoteGraphs[repoFullName]
	if !ok {
		return nil, fmt.Errorf("Error: repository %s not found in cross-repo match list", repoFullName)
	}
	return graph, nil
}

func matchesRemoteImportReference(entry codegraph.RemoteFileEntry, ref codegraph.Reference, importPath string) bool {
	if ref.Qualifier != "" {
		if candidate, ok := entry.ImportAliases[ref.Qualifier]; ok && candidate == importPath {
			return true
		}
	}
	if candidate, ok := entry.ImportAliases[ref.Symbol]; ok && candidate == importPath {
		return true
	}
	return false
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:0]
	last := ""
	for _, value := range values {
		if value == last {
			continue
		}
		out = append(out, value)
		last = value
	}
	return out
}
