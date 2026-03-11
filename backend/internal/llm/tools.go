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
			Description: "Find which file defines a symbol inside a remote repository.",
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
			Description: "Search the lightweight graph of a remote repository to find package imports or simple types.",
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
	if def == nil {
		return agents.ToolCallResponse{
			Name:    "get_symbol_definition",
			Content: fmt.Sprintf("Symbol '%s' not found in code graph index.", symbol),
		}
	}

	result := fmt.Sprintf("Symbol: %s\nFile: %s\nKind: %s\nLine: %d\n", symbol, path, def.Kind, def.Line)

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
			if len(snippet) > 3000 {
				snippet = snippet[:3000] + "\n// ... (truncated)"
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
	// Cap file content to prevent massive responses
	const maxChars = 20000
	if len(content) > maxChars {
		content = content[:maxChars] + "\n... (truncated at 20K chars)"
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

	// Find all files that reference this symbol
	var callers []string
	for path, entry := range te.Graph.Files {
		for _, ref := range entry.References {
			if ref.Symbol == symbol {
				callers = append(callers, fmt.Sprintf("  %s (kind: %s)", path, ref.Kind))
				break // one entry per file
			}
		}
	}

	if len(callers) == 0 {
		return agents.ToolCallResponse{
			Name:    "get_callers",
			Content: fmt.Sprintf("No callers/references found for symbol '%s'.", symbol),
		}
	}

	return agents.ToolCallResponse{
		Name:    "get_callers",
		Content: fmt.Sprintf("Symbol '%s' is referenced in %d files:\n%s", symbol, len(callers), strings.Join(callers, "\n")),
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

	if te.RemoteGraphs == nil {
		return agents.ToolCallResponse{Name: "resolve_repo_symbol", Content: "Error: remote graphs not initialized"}
	}

	graph, ok := te.RemoteGraphs[repoFullName]
	if !ok {
		return agents.ToolCallResponse{Name: "resolve_repo_symbol", Content: fmt.Sprintf("Error: repository %s not found in cross-repo match list", repoFullName)}
	}

	var found []string
	for path, entry := range graph.Files {
		for _, def := range entry.Definitions {
			if def.Symbol == symbol {
				found = append(found, fmt.Sprintf("File: %s (Lines %d-%d, Kind: %s)", path, def.Line, def.EndLine, def.Kind))
			}
		}
	}

	if len(found) == 0 {
		return agents.ToolCallResponse{Name: "resolve_repo_symbol", Content: fmt.Sprintf("Symbol %s not found in remote repository %s", symbol, repoFullName)}
	}

	return agents.ToolCallResponse{
		Name:    "resolve_repo_symbol",
		Content: "Found definitions:\n" + strings.Join(found, "\n"),
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

	graph, ok := te.RemoteGraphs[repoFullName]
	if !ok {
		return agents.ToolCallResponse{Name: "search_repo_graph", Content: fmt.Sprintf("Error: repository %s not found", repoFullName)}
	}

	queryLower := strings.ToLower(query)
	var matches []string

	for path, entry := range graph.Files {
		// Search definitions
		for _, def := range entry.Definitions {
			if strings.Contains(strings.ToLower(def.Symbol), queryLower) {
				matches = append(matches, fmt.Sprintf("[DEF] %s at %s:%d (%s)", def.Symbol, path, def.Line, def.Kind))
			}
		}
		// Search imports
		for _, imp := range entry.Imports {
			if strings.Contains(strings.ToLower(imp), queryLower) {
				matches = append(matches, fmt.Sprintf("[IMPORT] %s in %s", imp, path))
			}
		}
	}

	if len(matches) == 0 {
		return agents.ToolCallResponse{Name: "search_repo_graph", Content: fmt.Sprintf("No graph nodes matched '%s' in %s", query, repoFullName)}
	}

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
