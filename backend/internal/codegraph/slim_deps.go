package codegraph

import (
	"sort"
	"strings"
)

// BuildSlimDependencyIndex creates a compact dependency index from the full
// dependency summaries. Instead of including code snippets (~2000 chars/file),
// it lists only file paths and symbol names (~50 chars/file).
//
// This encourages agents to use tool calls (get_symbol_definition) to fetch
// actual code when needed, reducing context inflation and attention dilution.
//
// Example output:
//
//	Available dependencies (use get_symbol_definition tool to inspect):
//	- auth/middleware.go: AuthMiddleware, ValidateToken, RequireAdmin
//	- models/user.go: User, UserRole, CreateUserRequest
func BuildSlimDependencyIndex(fullDeps map[string]string) map[string]string {
	if len(fullDeps) == 0 {
		return nil
	}

	paths := make([]string, 0, len(fullDeps))
	for p := range fullDeps {
		// Skip internal graph summaries — they're not file-level deps
		if strings.HasPrefix(p, "_codegraph/") {
			continue
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// Preserve context graph summaries as-is (they're already compact)
	result := make(map[string]string)
	for p, content := range fullDeps {
		if strings.HasPrefix(p, "_codegraph/") {
			result[p] = content
		}
	}

	// Build the slim file-level index (if any file-level deps exist)
	if len(paths) > 0 {
		var b strings.Builder
		b.WriteString("Available dependencies (use get_symbol_definition tool to inspect):\n")

		for _, path := range paths {
			content := fullDeps[path]
			symbols := extractSymbolsFromSummary(content)
			if len(symbols) == 0 {
				b.WriteString("- ")
				b.WriteString(path)
				b.WriteString("\n")
			} else {
				b.WriteString("- ")
				b.WriteString(path)
				b.WriteString(": ")
				b.WriteString(strings.Join(symbols, ", "))
				b.WriteString("\n")
			}
		}

		result["_codegraph/dependency_index"] = b.String()
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// extractSymbolsFromSummary parses symbol names from a dependency summary.
// Summaries follow the format: "DEPENDENCY: path (resolves: Sym1, Sym2, ...)"
func extractSymbolsFromSummary(summary string) []string {
	// Look for "resolves: X, Y, Z)" pattern
	idx := strings.Index(summary, "resolves: ")
	if idx == -1 {
		return nil
	}

	after := summary[idx+len("resolves: "):]
	endIdx := strings.Index(after, ")")
	if endIdx == -1 {
		endIdx = strings.Index(after, "\n")
		if endIdx == -1 {
			endIdx = len(after)
		}
	}

	symbolStr := after[:endIdx]
	parts := strings.Split(symbolStr, ", ")
	var symbols []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && p != "..." {
			symbols = append(symbols, p)
		}
	}
	return symbols
}
