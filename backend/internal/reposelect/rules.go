package reposelect

import (
	"strings"

	"code-review/backend/internal/codegraph"
)

// IsExported determines if a symbol name is considered public/exported in the given language.
func IsExported(language string, symbol string) bool {
	if len(symbol) == 0 {
		return false
	}

	lng := strings.ToLower(language)
	switch lng {
	case "go":
		// Go: starts with capital letter
		return symbol[0] >= 'A' && symbol[0] <= 'Z'

	case "python", "javascript", "typescript", "java":
		// Simple cross-language heuristic: ignore private symbols starting with underscore
		return symbol[0] != '_'

	default:
		// Default to true if we don't know the language, better to over-match than under-match
		return symbol[0] != '_'
	}
}

// IsStandardLibrary determines if an import path belongs to the standard library or is a ubiquitous
// utility package that should be ignored for cross-repo matching.
func IsStandardLibrary(language string, importPath string) bool {
	return codegraph.IsStandardLibraryImport(language, importPath)
}
