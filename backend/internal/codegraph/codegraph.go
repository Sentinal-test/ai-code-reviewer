package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Service struct {
	RepoPath string
	Parser   *Parser
	Scanner  *Scanner
}

func NewService(repoPath string) *Service {
	return &Service{
		RepoPath: repoPath,
		Parser:   NewParser(),
		Scanner:  NewScanner(repoPath),
	}
}

// GetContext analyzes the changed files and returns a list of related files that provide context (definitions).
// It returns a map of file path -> content.
func (s *Service) GetContext(ctx context.Context, changedFiles map[string]string) (map[string]string, error) {
	fmt.Println("🔍 [CodeGraph] Starting deterministic context analysis...")

	referencedSymbols := make(map[string]string) // symbol -> lang

	// 1. Extract references from changed files
	for path, content := range changedFiles {
		langConfig, ok := GetLanguageForFile(path)
		if !ok {
			continue
		}

		fmt.Printf("🔍 [CodeGraph] Parsing changed file: %s (%s)\n", path, langConfig.Name)
		root, err := s.Parser.ParseFile(ctx, []byte(content), langConfig)
		if err != nil {
			fmt.Printf("⚠️ [CodeGraph] Failed to parse %s: %v\n", path, err)
			continue
		}

		refs, err := s.Parser.ExtractReferences(root, []byte(content), strings.ToLower(langConfig.Name))
		if err != nil {
			fmt.Printf("⚠️ [CodeGraph] Failed to extract refs from %s: %v\n", path, err)
			continue
		}

		for _, ref := range refs {
			referencedSymbols[ref] = strings.ToLower(langConfig.Name)
		}
	}

	fmt.Printf("🔍 [CodeGraph] Found %d unique referenced symbols.\n", len(referencedSymbols))

	// 2. Find definitions for these symbols
	// We'll use a concurrent approach for searching
	foundFiles := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	semaphore := make(chan struct{}, 10) // Limit concurrency

	for symbol, lang := range referencedSymbols {
		wg.Add(1)
		go func(sym, l string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Find candidates
			langConfig, _ := SupportedLanguages[l]
			candidates, err := s.Scanner.FindCandidates(ctx, sym, langConfig.Extensions)
			if err != nil {
				return
			}

			for _, candidatePath := range candidates {
				// Avoid checking files we already have (either changed or found)
				mu.Lock()
				if _, exists := changedFiles[candidatePath]; exists {
					mu.Unlock()
					continue
				}
				if _, exists := foundFiles[candidatePath]; exists {
					mu.Unlock()
					continue
				}
				mu.Unlock()

				// Read file content
				fullPath := filepath.Join(s.RepoPath, candidatePath)
				contentBytes, err := os.ReadFile(fullPath)
				if err != nil {
					continue
				}

				// Parse and check for definition
				candidateLang, ok := GetLanguageForFile(candidatePath)
				if !ok {
					continue
				}

				root, err := s.Parser.ParseFile(ctx, contentBytes, candidateLang)
				if err != nil {
					continue
				}

				defs, err := s.Parser.ExtractDefinitions(root, contentBytes, strings.ToLower(candidateLang.Name))
				if err != nil {
					continue
				}

				for _, def := range defs {
					if def == sym {
						// Found it!
						mu.Lock()
						if _, exists := foundFiles[candidatePath]; !exists {
							foundFiles[candidatePath] = string(contentBytes)
							fmt.Printf("✅ [CodeGraph] Found definition for '%s' in %s\n", sym, candidatePath)
						}
						mu.Unlock()
						break
					}
				}
			}
		}(symbol, lang)
	}

	wg.Wait()

	fmt.Printf("✅ [CodeGraph] Analysis complete. Found %d related context files.\n", len(foundFiles))
	return foundFiles, nil
}
