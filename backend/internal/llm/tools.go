package llm

import (
	"code-review/backend/internal/agents"
	"code-review/backend/internal/codegraph"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AgentToolDeclarations returns the Gemini function_declarations for agent tool calls.
func AgentToolDeclarations() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "get_symbol_definition",
			"description": "Get the source code definition of a symbol (function, type, struct, class, interface). Use this when you see a symbol in the diff that you need to understand.",
			"parameters": map[string]interface{}{
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
			"name":        "get_file_content",
			"description": "Get the full content of a file from the repository. Use this to inspect files not included in the diff.",
			"parameters": map[string]interface{}{
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
			"name":        "get_callers",
			"description": "Find all files and locations that reference or call a given symbol. Use this to check if a function is used elsewhere.",
			"parameters": map[string]interface{}{
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
			"name":        "search_codebase",
			"description": "Search the codebase for a text pattern using git grep. Returns matching lines with file paths.",
			"parameters": map[string]interface{}{
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
	}
}

// ToolExecutor handles executing tool calls locally using the code graph and filesystem.
type ToolExecutor struct {
	RepoPath string
	Graph    *codegraph.Graph
}

// NewToolExecutor creates a tool executor for the given repo.
func NewToolExecutor(repoPath string, graph *codegraph.Graph) *ToolExecutor {
	return &ToolExecutor{
		RepoPath: repoPath,
		Graph:    graph,
	}
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
	if def.Snippet != "" {
		result += fmt.Sprintf("\nSource:\n%s", def.Snippet)
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
