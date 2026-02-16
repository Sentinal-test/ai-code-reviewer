package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// MaxDepContextChars caps total dependency context to ~12k tokens
const MaxDepContextChars = 50000

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

// isBuiltinSymbol returns true if a symbol is a language primitive, stdlib function,
// or too short/generic to be worth searching for definitions.
func isBuiltinSymbol(symbol string) bool {
	// Too short — variables like ok, r, w, db, ctx, id
	if len(symbol) <= 2 {
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

		// Go fmt/log package (extremely common, never local definitions)
		"Printf": true, "Println": true, "Sprintf": true, "Fprintf": true,
		"Errorf": true, "Print": true, "Sprint": true, "Sprintln": true,
		"Fatalf": true, "Fatal": true, "Fatalln": true,

		// Go os/env
		"Getenv": true, "Exit": true, "Setenv": true,

		// Go common stdlib types/funcs that are never project-local
		"Context": true, "Background": true, "TODO": true, "WithCancel": true,
		"WithTimeout": true, "WithDeadline": true, "WithValue": true,
		"Writer": true, "Reader": true, "Closer": true, "ReadCloser": true,
		"WriteCloser": true, "ReadWriter": true, "ReadWriteCloser": true,
		"ResponseWriter": true, "Request": true, "Handler": true, "HandlerFunc": true,
		"Header": true, "StatusOK": true, "StatusBadRequest": true, "StatusNotFound": true,
		"StatusInternalServerError": true, "StatusUnauthorized": true, "StatusForbidden": true,

		// Go common operations
		"Error": true, "String": true, "Bytes": true,
		"Close": true, "Read": true, "Write": true, "Flush": true,
		"Lock": true, "Unlock": true, "RLock": true, "RUnlock": true,
		"Add": true, "Done": true, "Wait": true,
		"Marshal": true, "Unmarshal": true, "Encode": true, "Decode": true,
		"NewDecoder": true, "NewEncoder": true,
		"ReadAll": true, "ReadFile": true, "WriteFile": true,
		"NewBuffer": true, "NewReader": true, "NewWriter": true,
		"Set": true, "Get": true,

		// Go string/path operations
		"Contains": true, "HasPrefix": true, "HasSuffix": true,
		"TrimSpace": true, "TrimPrefix": true, "TrimSuffix": true, "Trim": true,
		"Split": true, "Join": true, "Replace": true, "ToLower": true, "ToUpper": true,
		"Fields": true, "Index": true, "Repeat": true,
		"Base": true, "Dir": true, "Ext": true, "Rel": true, "Abs": true,
		"Sscanf": true, "Scanf": true,

		// Go sync / concurrency
		"Mutex": true, "RWMutex": true, "WaitGroup": true, "Once": true,

		// Go net/http (already covered above but being thorough)
		"Do": true, "ListenAndServe": true,
		"NewRequestWithContext": true, "NewRequest": true,

		// Go testing
		"NoError": true, "NotNil": true, "Equal": true, "True": true, "False": true,
		"Nil": true, "Len": true, "Run": true,

		// Go regex
		"MustCompile": true, "FindStringSubmatch": true, "MatchString": true,

		// Go misc
		"WriteString": true, "Builder": true, "Buffer": true,
		"ValidString": true, "Walk": true, "IsDir": true,
		"Name": true, "Size": true, "Mode": true,
		"Exec": true, "LastInsertId": true, "Scan": true, "QueryRow": true,
		"URLParam": true, "Route": true,
		"WriteHeader":    true,
		"CommandContext": true,

		// Common short variable-like symbols
		"ctx": true, "err": true, "nil": true, "true": true, "false": true,
		"cmd": true, "buf": true, "req": true, "res": true,
		"msg": true, "key": true, "val": true, "out": true,

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
		"push": true, "pop": true, "shift": true, "unshift": true,
		"forEach": true, "reduce": true, "find": true, "some": true, "every": true,
		"keys": true, "values": true, "entries": true,
		"toString": true, "valueOf": true, "constructor": true, "prototype": true,
		"length": true, "splice": true, "slice": true, "concat": true,
		"includes": true, "indexOf": true, "startsWith": true, "endsWith": true,
		"trim": true, "replace": true, "match": true, "test": true, "split": true,

		// Java builtins
		"System": true, "Integer": true, "Long": true, "Double": true, "Float": true,
		"Boolean": true, "Character": true, "Byte": true, "Short": true,
		"List": true, "ArrayList": true, "HashMap": true, "HashSet": true,
		"Collections": true, "Arrays": true, "Stream": true, "Optional": true,
		"Collectors": true, "Iterator": true, "Iterable": true,
		"Exception": true, "RuntimeException": true, "IOException": true,
		"NullPointerException": true, "IllegalArgumentException": true,
		"Override": true, "Deprecated": true, "SuppressWarnings": true,
	}

	return builtins[symbol]
}

// GetContext analyzes the changed files and returns a map of file path -> relevant definition snippets.
func (s *Service) GetContext(ctx context.Context, changedFiles map[string]string) (map[string]string, error) {
	fmt.Println("🔍 [CodeGraph] Starting deterministic context analysis...")

	referencedSymbols := make(map[string]string) // symbol -> lang

	// 1. Extract references from changed files
	fmt.Println("📝 [CodeGraph] Input: Analyzing the following changed files:")
	for path := range changedFiles {
		fmt.Printf("  - %s\n", path)
	}

	for path, content := range changedFiles {
		langConfig, ok := GetLanguageForFile(path)
		if !ok {
			continue
		}

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

		// Filter out builtins before adding
		filtered := 0
		for _, ref := range refs {
			if !isBuiltinSymbol(ref) {
				referencedSymbols[ref] = strings.ToLower(langConfig.Name)
			} else {
				filtered++
			}
		}

		if len(refs) > 0 {
			fmt.Printf("   -> Found %d references in %s (%d builtin filtered)\n", len(refs)-filtered, path, filtered)
		}
	}

	fmt.Printf("🔍 [CodeGraph] Searching for %d unique project symbols...\n", len(referencedSymbols))
	if len(referencedSymbols) > 0 {
		symList := make([]string, 0, len(referencedSymbols))
		for s := range referencedSymbols {
			symList = append(symList, s)
		}
		fmt.Printf("   Symbols: %v\n", symList)
	}

	// 2. Find definitions and extract only the relevant snippets
	type snippetEntry struct {
		path    string
		snippet string
	}

	foundSnippets := make(map[string]string) // path -> accumulated snippets
	var mu sync.Mutex
	var wg sync.WaitGroup
	totalContextSize := 0

	semaphore := make(chan struct{}, 10)

	for symbol, lang := range referencedSymbols {
		wg.Add(1)
		go func(sym, l string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Check context budget
			mu.Lock()
			if totalContextSize >= MaxDepContextChars {
				mu.Unlock()
				return
			}
			mu.Unlock()

			langConfig, _ := SupportedLanguages[l]
			candidates, err := s.Scanner.FindCandidates(ctx, sym, langConfig.Extensions)
			if err != nil {
				return
			}

			for _, candidatePath := range candidates {
				mu.Lock()
				if _, exists := changedFiles[candidatePath]; exists {
					mu.Unlock()
					continue
				}
				if totalContextSize >= MaxDepContextChars {
					mu.Unlock()
					return
				}
				mu.Unlock()

				// Read file content
				fullPath := filepath.Join(s.RepoPath, candidatePath)
				contentBytes, err := os.ReadFile(fullPath)
				if err != nil {
					continue
				}

				candidateLang, ok := GetLanguageForFile(candidatePath)
				if !ok {
					continue
				}

				root, err := s.Parser.ParseFile(ctx, contentBytes, candidateLang)
				if err != nil {
					continue
				}

				// Extract ONLY the matching definition snippet, not the full file
				snippet, err := s.Parser.ExtractDefinitionSnippet(root, contentBytes, strings.ToLower(candidateLang.Name), sym)
				if err != nil || snippet == "" {
					continue
				}

				mu.Lock()
				if totalContextSize >= MaxDepContextChars {
					mu.Unlock()
					return
				}

				// Append snippet to this file's accumulated snippets
				existing := foundSnippets[candidatePath]
				if existing != "" {
					// Check if this snippet is already included (dedup)
					if !strings.Contains(existing, snippet) {
						newContent := existing + "\n\n" + snippet
						addedSize := len(snippet) + 2
						if totalContextSize+addedSize <= MaxDepContextChars {
							foundSnippets[candidatePath] = newContent
							totalContextSize += addedSize

							// Detailed logging
							preview := snippet
							if len(preview) > 60 {
								preview = preview[:57] + "..."
							}
							preview = strings.ReplaceAll(preview, "\n", " ")
							fmt.Printf("✅ [CodeGraph] Found definition for '%s' in %s\n", sym, candidatePath)
							fmt.Printf("   Snippet: %s\n", preview)
						}
					}
				} else {
					// First snippet for this file — add file header
					header := fmt.Sprintf("// From: %s\n", candidatePath)
					newContent := header + snippet
					addedSize := len(newContent)
					if totalContextSize+addedSize <= MaxDepContextChars {
						foundSnippets[candidatePath] = newContent
						totalContextSize += addedSize

						// Detailed logging
						preview := snippet
						if len(preview) > 60 {
							preview = preview[:57] + "..."
						}
						preview = strings.ReplaceAll(preview, "\n", " ")
						fmt.Printf("✅ [CodeGraph] Found definition for '%s' in %s\n", sym, candidatePath)
						fmt.Printf("   Snippet: %s\n", preview)
					}
				}
				mu.Unlock()
			}
		}(symbol, lang)
	}

	wg.Wait()

	fmt.Printf("✅ [CodeGraph] Analysis complete. Found definitions in %d files (%d chars of context).\n", len(foundSnippets), totalContextSize)
	if len(foundSnippets) > 0 {
		fmt.Println("📂 [CodeGraph] Output: Added the following files to context:")
		for path := range foundSnippets {
			fmt.Printf("  + %s\n", path)
		}
	}
	return foundSnippets, nil
}
