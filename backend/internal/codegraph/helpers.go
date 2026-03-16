package codegraph

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// isBuiltinSymbol returns true if a symbol is a language primitive or stdlib function.
func isBuiltinSymbol(symbol string) bool {
	// Only filter extremely short single-character noise
	if len(symbol) <= 1 {
		return true
	}

	builtins := map[string]bool{
		// Go primitive types
		"string": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
		"float32": true, "float64": true, "complex64": true, "complex128": true,
		"bool": true, "byte": true, "rune": true, "error": true, "uintptr": true,
		"any": true, "comparable": true,

		// Go builtin functions
		"len": true, "cap": true, "make": true, "new": true, "append": true,
		"copy": true, "delete": true, "close": true, "panic": true, "recover": true,
		"print": true, "println": true, "real": true, "imag": true, "complex": true,
		"clear": true, "min": true, "max": true,

		// Python builtins
		"range": true, "self": true, "None": true, "cls": true,
		"super": true, "type": true, "list": true, "dict": true,
		"set": true, "tuple": true, "str": true, "map": true,
		"filter": true, "sorted": true, "enumerate": true,
		"isinstance": true, "issubclass": true, "getattr": true, "setattr": true,
		"hasattr": true, "property": true, "staticmethod": true, "classmethod": true,

		// JS/TS builtins
		"console": true, "log": true, "warn": true, "info": true, "debug": true,
		"setTimeout": true, "setInterval": true, "clearTimeout": true, "clearInterval": true,
		"Promise": true, "Array": true, "Object": true, "Map": true,
		"JSON": true, "Math": true, "Date": true, "RegExp": true, "Symbol": true,
		"parseInt": true, "parseFloat": true, "isNaN": true, "isFinite": true,
		"require": true, "module": true, "exports": true,
		"document": true, "window": true, "global": true, "process": true,
		"then": true, "catch": true, "finally": true, "async": true, "await": true,

		// Java builtins
		"System": true, "Integer": true, "Long": true, "Double": true, "Float": true,
		"Boolean": true, "Character": true, "Byte": true, "Short": true,
		"Override": true, "Deprecated": true, "SuppressWarnings": true,
	}

	return builtins[symbol]
}

func isTestFile(path string) bool {
	lower := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasSuffix(lower, "_test.go"):
		return true
	case strings.HasSuffix(lower, "_test.py"):
		return true
	case strings.HasSuffix(lower, ".test.js"), strings.HasSuffix(lower, ".spec.js"):
		return true
	case strings.HasSuffix(lower, ".test.ts"), strings.HasSuffix(lower, ".spec.ts"):
		return true
	case strings.HasSuffix(lower, ".test.jsx"), strings.HasSuffix(lower, ".spec.jsx"):
		return true
	case strings.HasSuffix(lower, ".test.tsx"), strings.HasSuffix(lower, ".spec.tsx"):
		return true
	case strings.HasSuffix(lower, "test.java"), strings.HasSuffix(lower, "tests.java"):
		return true
	}
	return false
}

// IsStandardLibraryImport determines if an import path belongs to the standard library
// or a ubiquitous framework dependency that should not be treated as a meaningful
// cross-repository signal.
func IsStandardLibraryImport(language string, importPath string) bool {
	if importPath == "" {
		return false
	}

	lng := strings.ToLower(language)
	switch lng {
	case "go":
		firstSegment := strings.Split(importPath, "/")[0]
		if !strings.Contains(firstSegment, ".") {
			return true
		}
		ubiquitousGo := map[string]bool{
			"github.com/sirupsen/logrus":  true,
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
		ubiquitousPy := map[string]bool{
			"pytest": true, "requests": true, "numpy": true, "pandas": true,
			"django": true, "flask": true, "pydantic": true, "black": true,
			"flake8": true, "tox": true,
		}
		return ubiquitousPy[firstSegment]
	case "javascript", "typescript":
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

func loadGoModulePath(repoPath string) string {
	if repoPath == "" {
		return ""
	}

	file, err := os.Open(filepath.Join(repoPath, "go.mod"))
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func namespaceFromPath(path string) string {
	clean := strings.TrimSuffix(filepath.ToSlash(path), filepath.Ext(path))
	clean = strings.TrimPrefix(clean, "./")
	return strings.ReplaceAll(clean, "/", ".")
}

func lastSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Trim(value, ".")
	value = strings.TrimSuffix(value, filepath.Ext(value))
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '.' })
	if len(parts) == 0 {
		return value
	}
	return parts[len(parts)-1]
}

func normalizeImportPath(langName, importPath string) string {
	normalized := strings.TrimSpace(strings.Trim(importPath, "\"'`"))
	if normalized == "" {
		return ""
	}
	switch langName {
	case "java":
		normalized = strings.TrimSuffix(normalized, ";")
	}
	return normalized
}

func importAliasForSpec(langName string, spec ImportSpec) string {
	alias := strings.TrimSpace(spec.Alias)
	if alias != "" && alias != "." && alias != "_" {
		return alias
	}

	path := normalizeImportPath(langName, spec.Path)
	if path == "" {
		return ""
	}

	switch langName {
	case "go", "java":
		return lastSegment(path)
	default:
		return ""
	}
}

func isResolvableProjectImport(repoPath string, graph *Graph, sourcePath, importPath, langName, goModulePath string) bool {
	importPath = normalizeImportPath(langName, importPath)
	if importPath == "" {
		return false
	}

	switch langName {
	case "go":
		if goModulePath != "" && strings.HasPrefix(importPath, goModulePath) {
			trimmed := strings.TrimPrefix(importPath, goModulePath)
			trimmed = strings.TrimPrefix(trimmed, "/")
			targetDir := filepath.Join(repoPath, filepath.FromSlash(trimmed))
			if hasProjectSourceFiles(targetDir) {
				return true
			}
		}
		return false
	case "python":
		if strings.HasPrefix(importPath, ".") {
			return true
		}
		target := filepath.Join(repoPath, filepath.FromSlash(strings.ReplaceAll(importPath, ".", "/")))
		if hasProjectSourceFiles(target) || hasProjectSourceFiles(target+".py") {
			return true
		}
		return hasPackageInGraph(graph, importPath)
	case "javascript", "typescript":
		if strings.HasPrefix(importPath, ".") {
			baseDir := filepath.Dir(sourcePath)
			target := filepath.Join(repoPath, filepath.FromSlash(baseDir), filepath.FromSlash(importPath))
			if hasProjectSourceFiles(target) {
				return true
			}
			for _, ext := range []string{".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"} {
				if hasProjectSourceFiles(target + ext) {
					return true
				}
			}
			return false
		}
		target := filepath.Join(repoPath, filepath.FromSlash(importPath))
		return hasProjectSourceFiles(target) || hasPackageInGraph(graph, namespaceFromPath(importPath))
	case "java":
		target := filepath.Join(repoPath, filepath.FromSlash(strings.ReplaceAll(importPath, ".", "/")))
		return hasProjectSourceFiles(target) || hasProjectSourceFiles(target+".java") || hasPackageInGraph(graph, importPath)
	default:
		return false
	}
}

func hasProjectSourceFiles(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return true
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := GetLanguageForFile(entry.Name()); ok {
			return true
		}
	}
	return false
}

func hasPackageInGraph(graph *Graph, pkg string) bool {
	if graph == nil || pkg == "" {
		return false
	}
	for _, file := range graph.Files {
		if file.Package == pkg {
			return true
		}
	}
	return false
}

// --- Sort / collection helpers ---

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedDependencyKeys(m map[string]*dependencyInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedSymbolSlice(symbols map[string]struct{}) []string {
	out := make([]string, 0, len(symbols))
	for s := range symbols {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func addReferenceUnique(existing []Reference, candidate Reference) []Reference {
	for _, item := range existing {
		if item.Symbol == candidate.Symbol && item.Kind == candidate.Kind {
			return existing
		}
	}
	return append(existing, candidate)
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

func displayList(values []string, limit int) string {
	if len(values) == 0 {
		return "none"
	}
	if len(values) <= limit {
		return strings.Join(values, ", ")
	}
	return strings.Join(values[:limit], ", ") + fmt.Sprintf(" (+%d more)", len(values)-limit)
}
