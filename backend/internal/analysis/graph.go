package analysis

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

type Index struct {
	Symbols map[string][]Symbol // Name -> List of symbols (multiple files can have same name)
	Files   map[string][]Symbol // Path -> List of symbols in file
}

func NewIndex() *Index {
	return &Index{
		Symbols: make(map[string][]Symbol),
		Files:   make(map[string][]Symbol),
	}
}

// BuildIndex scans the repository and indexes all symbols
func (idx *Index) BuildIndex(ctx context.Context, root string) error {
	extractor := NewExtractor()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		if ext == ".go" || ext == ".py" || ext == ".ts" || ext == ".js" || ext == ".tsx" || ext == ".jsx" {
			relPath, _ := filepath.Rel(root, path)
			syms, err := extractor.ExtractSymbols(ctx, path)
			if err != nil {
				// Log error but continue
				fmt.Printf("Error indexing %s: %v\n", path, err)
				return nil
			}

			idx.Files[relPath] = syms
			for _, s := range syms {
				lowerName := strings.ToLower(s.Name)
				idx.Symbols[lowerName] = append(idx.Symbols[lowerName], s)
			}
		}
		return nil
	})

	return err
}

// GetRequiredContext analyzes a diff and optionally PR context to determine which symbols/files are needed
func (idx *Index) GetRequiredContext(diff string, prTitle, prBody string) map[string]string {
	// 1. Extract tokens that look like function calls, type usages, or keywords
	tokens := extractTokensFromDiff(diff)

	// Also extract from PR context (Intent detection)
	prTokens := extractTokensFromText(prTitle + " " + prBody)
	tokens = append(tokens, prTokens...)

	needed := make(map[string][]Symbol)
	seen := make(map[string]bool)

	for _, t := range tokens {
		lowerT := strings.ToLower(t)
		if seen[lowerT] {
			continue
		}
		seen[lowerT] = true

		// Match Symbols (Case-insensitive)
		if syms, ok := idx.Symbols[lowerT]; ok {
			for _, s := range syms {
				needed[s.File] = append(needed[s.File], s)
			}
		}

		// Match File Names (e.g., if PR says "client", find "internal/db/client.go")
		for path, syms := range idx.Files {
			base := strings.ToLower(filepath.Base(path))
			if strings.Contains(base, lowerT) {
				// Don't add duplicate symbols if we already have them for this file
				// but ensure at least some symbols from this file are included
				if _, already := needed[path]; !already {
					needed[path] = syms
				}
			}
		}
	}

	// 2. Format the context
	// We want to give the LLM a concise view: "In file X, these symbols are defined: ..."
	contextMap := make(map[string]string)
	for path, syms := range needed {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("// CONTEXT FROM DEPENDENCY: %s\n", path))
		for _, s := range syms {
			// Always provide full body for Structs and Interfaces
			if s.Type == SymbolStruct || s.Type == SymbolInterface {
				b.WriteString(fmt.Sprintf("// %s definition:\n%s\n\n", s.Type, s.Body))
			} else {
				// For functions, provide signature and body if it's manageable (e.g. < 50 lines)
				b.WriteString(fmt.Sprintf("// %s definition:\n%s", s.Type, s.Signature))
				if s.LineEnd-s.LineStart < 50 {
					b.WriteString(" {\n")
					// Strip the signature portion from the body to avoid duplication if possible,
					// but for now just use the body
					b.WriteString(s.Body)
					b.WriteString("\n}\n\n")
				} else {
					b.WriteString("\n// (Body omitted for brevity. Significant complexity found.)\n\n")
				}
			}
		}
		contextMap[path] = b.String()
	}

	return contextMap
}

func extractTokensFromDiff(diff string) []string {
	tokens := make(map[string]bool)
	lines := strings.Split(diff, "\n")

	for _, line := range lines {
		if !strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "-") {
			continue
		}

		text := line[1:]
		for _, t := range extractTokensFromText(text) {
			tokens[t] = true
		}
	}

	res := make([]string, 0, len(tokens))
	for t := range tokens {
		res = append(res, t)
	}
	return res
}

func extractTokensFromText(text string) []string {
	tokens := make(map[string]bool)

	// Identifier logic: split by anything non-alphanumeric/underscore
	f := func(c rune) bool {
		return !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_')
	}

	words := strings.FieldsFunc(text, f)
	for _, w := range words {
		if len(w) > 2 {
			tokens[w] = true
			// Also support split by CamelCase for sub-tokens
			subTokens := splitCamelCase(w)
			for _, st := range subTokens {
				if len(st) > 2 {
					tokens[st] = true
				}
			}
		}
	}

	res := make([]string, 0, len(tokens))
	for t := range tokens {
		res = append(res, t)
	}
	return res
}

func splitCamelCase(s string) []string {
	var result []string
	var current strings.Builder

	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result = append(result, current.String())
			current.Reset()
		}
		current.WriteRune(r)
	}
	result = append(result, current.String())
	return result
}
