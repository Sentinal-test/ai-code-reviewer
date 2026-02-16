package codegraph

import (
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

type LanguageConfig struct {
	Name       string
	Extensions []string
	Grammar    *sitter.Language
}

var SupportedLanguages = map[string]LanguageConfig{
	"go": {
		Name:       "Go",
		Extensions: []string{".go"},
		Grammar:    golang.GetLanguage(),
	},
	"javascript": {
		Name:       "JavaScript",
		Extensions: []string{".js", ".jsx", ".mjs", ".cjs"},
		Grammar:    javascript.GetLanguage(),
	},
	"typescript": {
		Name:       "TypeScript",
		Extensions: []string{".ts", ".tsx"},
		Grammar:    typescript.GetLanguage(),
	},
	"python": {
		Name:       "Python",
		Extensions: []string{".py"},
		Grammar:    python.GetLanguage(),
	},
	"java": {
		Name:       "Java",
		Extensions: []string{".java"},
		Grammar:    java.GetLanguage(),
	},
}

func GetLanguageForFile(path string) (*LanguageConfig, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, config := range SupportedLanguages {
		for _, e := range config.Extensions {
			if e == ext {
				return &config, true
			}
		}
	}
	return nil, false
}
