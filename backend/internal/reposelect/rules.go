package reposelect

import (
	"strings"
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

// IsStandardLibrary determines if an import path belongs to the standard library/built-ins of the given language.
func IsStandardLibrary(language string, importPath string) bool {
	if importPath == "" {
		return false
	}

	lng := strings.ToLower(language)
	switch lng {
	case "go":
		// Go stdlib packages typically don't have a dot in the first segment (e.g. "fmt", "net/http")
		firstSegment := strings.Split(importPath, "/")[0]
		return !strings.Contains(firstSegment, ".")

	case "python":
		// Common python stdlib top-level modules
		firstSegment := strings.Split(strings.Split(importPath, ".")[0], " ")[0]
		pyStdlib := map[string]bool{
			"os": true, "sys": true, "json": true, "re": true, "datetime": true,
			"math": true, "typing": true, "collections": true, "functools": true,
			"itertools": true, "pathlib": true, "random": true, "time": true,
			"subprocess": true, "logging": true, "asyncio": true, "urllib": true,
			"hashlib": true, "socket": true,
		}
		return pyStdlib[firstSegment]

	case "javascript", "typescript":
		// Node.js built-ins often start with "node:" or are explicitly named
		if strings.HasPrefix(importPath, "node:") {
			return true
		}
		firstSegment := strings.Split(importPath, "/")[0]
		jsBuiltins := map[string]bool{
			"fs": true, "path": true, "http": true, "crypto": true, "os": true,
			"util": true, "events": true, "stream": true, "url": true, "child_process": true,
		}
		return jsBuiltins[firstSegment]

	case "java":
		return strings.HasPrefix(importPath, "java.") || strings.HasPrefix(importPath, "javax.")

	default:
		return false
	}
}
