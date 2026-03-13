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

// IsStandardLibrary determines if an import path belongs to the standard library or is a ubiquitous 
// utility package that should be ignored for cross-repo matching.
func IsStandardLibrary(language string, importPath string) bool {
	if importPath == "" {
		return false
	}

	lng := strings.ToLower(language)
	switch lng {
	case "go":
		// Go stdlib packages typically don't have a dot in the first segment (e.g. "fmt", "net/http")
		firstSegment := strings.Split(importPath, "/")[0]
		if !strings.Contains(firstSegment, ".") {
			return true
		}
		// Ubiquitous third-party Go packages
		ubiquitousGo := map[string]bool{
			"github.com/sirupsen/logrus": true,
			"github.com/stretchr/testify": true,
			"github.com/spf13/cobra":      true,
			"github.com/spf13/viper":      true,
			"github.com/onsi/ginkgo":      true,
			"github.com/onsi/gomega":      true,
			"go.uber.org/zap":             true,
			"github.com/rs/zerolog":       true,
		}
		return ubiquitousGo[importPath] || strings.HasPrefix(importPath, "github.com/stretchr/testify/")

	case "python":
		// Common python stdlib top-level modules
		firstSegment := strings.Split(strings.Split(importPath, ".")[0], " ")[0]
		pyStdlib := map[string]bool{
			"os": true, "sys": true, "json": true, "re": true, "datetime": true,
			"math": true, "typing": true, "collections": true, "functools": true,
			"itertools": true, "pathlib": true, "random": true, "time": true,
			"subprocess": true, "logging": true, "asyncio": true, "urllib": true,
			"hashlib": true, "socket": true, "abc": true, "argparse": true,
			"base64": true, "copy": true, "csv": true, "enum": true, "glob": true,
			"io": true, "pickle": true, "shutil": true, "sqlite3": true,
			"tempfile": true, "threading": true, "uuid": true, "warnings": true,
		}
		if pyStdlib[firstSegment] {
			return true
		}
		// Ubiquitous Python packages
		ubiquitousPy := map[string]bool{
			"pytest": true, "requests": true, "numpy": true, "pandas": true,
			"django": true, "flask": true, "pydantic": true, "black": true,
			"flake8": true, "tox": true,
		}
		return ubiquitousPy[firstSegment]

	case "javascript", "typescript":
		// Node.js built-ins often start with "node:" or are explicitly named
		if strings.HasPrefix(importPath, "node:") {
			return true
		}
		firstSegment := strings.Split(importPath, "/")[0]
		if strings.HasPrefix(firstSegment, "@types/") {
			return true
		}
		jsBuiltins := map[string]bool{
			"fs": true, "path": true, "http": true, "crypto": true, "os": true,
			"util": true, "events": true, "stream": true, "url": true, "child_process": true,
			"assert": true, "buffer": true, "console": true, "dns": true, "net": true,
			"querystring": true, "readline": true, "tls": true, "zlib": true,
		}
		if jsBuiltins[firstSegment] {
			return true
		}
		// Ubiquitous JS/TS packages
		ubiquitousJS := map[string]bool{
			"jest": true, "mocha": true, "chai": true, "eslint": true, "prettier": true,
			"lodash": true, "axios": true, "react": true, "vue": true, "express": true,
			"typescript": true, "next": true,
		}
		return ubiquitousJS[firstSegment]

	case "java":
		if strings.HasPrefix(importPath, "java.") || strings.HasPrefix(importPath, "javax.") {
			return true
		}
		// Ubiquitous Java packages
		ubiquitousJava := []string{
			"org.junit.", "org.slf4j.", "org.apache.log4j.", "org.mockito.", "com.google.common.",
		}
		for _, prefix := range ubiquitousJava {
			if strings.HasPrefix(importPath, prefix) {
				return true
			}
		}
		return false

	default:
		return false
	}
}
