package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// MaxDepContextChars keeps dependency summaries compact for the LLM context window.
const MaxDepContextChars = 12000

// MaxSymbolsPerSummary limits per-file symbol lists to avoid verbose summaries.
const MaxSymbolsPerSummary = 8

// MaxSnippetsPerFile caps captured snippets used only for deterministic data-flow inference.
const MaxSnippetsPerFile = 6

// MaxBehaviorSignals caps inferred behavior hints per summary.
const MaxBehaviorSignals = 4

// MaxGraphEdgeSymbols limits symbol examples shown per graph edge.
const MaxGraphEdgeSymbols = 4

// MaxDataFlowLines limits emitted data-flow chains.
const MaxDataFlowLines = 12

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

type dependencyInfo struct {
	Symbols     map[string]struct{}
	Snippets    []string
	Definitions map[string]struct{}
	References  []Reference
}

// GetContext analyzes changed files and returns compact summaries:
// - per dependency file behavior summary
// - a context-graph relationship summary with data flow
func (s *Service) GetContext(ctx context.Context, changedFiles map[string]string) (map[string]string, error) {
	fmt.Println("🔍 [CodeGraph] Starting deterministic context analysis...")

	referencedSymbols := make(map[string]string) // symbol -> lang
	refsByChangedFile := make(map[string][]Reference)

	// 1. Extract references from changed files
	fmt.Println("📝 [CodeGraph] Input: Analyzing the following changed files:")
	for _, path := range sortedMapKeys(changedFiles) {
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

		referenceDetails, err := s.Parser.ExtractReferenceDetails(root, []byte(content), strings.ToLower(langConfig.Name))
		if err != nil {
			fmt.Printf("⚠️ [CodeGraph] Failed to extract refs from %s: %v\n", path, err)
			continue
		}

		// Filter out builtins before adding
		filtered := 0
		for _, ref := range referenceDetails {
			if isBuiltinSymbol(ref.Symbol) {
				filtered++
				continue
			}

			referencedSymbols[ref.Symbol] = strings.ToLower(langConfig.Name)
			refsByChangedFile[path] = addReferenceUnique(refsByChangedFile[path], ref)
		}

		if len(referenceDetails) > 0 {
			fmt.Printf("   -> Found %d references in %s (%d builtin filtered)\n", len(referenceDetails)-filtered, path, filtered)
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

	// 2. Find definitions and extract only enough detail to build summaries
	foundDeps := make(map[string]*dependencyInfo)
	symbolToDefPath := make(map[string]string) // symbol -> dependency file path
	var mu sync.Mutex
	var wg sync.WaitGroup

	semaphore := make(chan struct{}, 10)

	for symbol, lang := range referencedSymbols {
		wg.Add(1)
		go func(sym, l string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

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
				if _, alreadyMapped := symbolToDefPath[sym]; !alreadyMapped {
					symbolToDefPath[sym] = candidatePath
				}

				dep := foundDeps[candidatePath]
				if dep == nil {
					dep = &dependencyInfo{
						Symbols:     make(map[string]struct{}),
						Definitions: make(map[string]struct{}),
					}
					foundDeps[candidatePath] = dep
				}
				dep.Symbols[sym] = struct{}{}
				if len(dep.Snippets) < MaxSnippetsPerFile && !containsString(dep.Snippets, snippet) {
					dep.Snippets = append(dep.Snippets, snippet)
				}

				preview := snippet
				if len(preview) > 60 {
					preview = preview[:57] + "..."
				}
				preview = strings.ReplaceAll(preview, "\n", " ")
				fmt.Printf("✅ [CodeGraph] Found definition for '%s' in %s\n", sym, candidatePath)
				fmt.Printf("   Snippet: %s\n", preview)
				mu.Unlock()

				// A single concrete definition is enough for deterministic context.
				break
			}
		}(symbol, lang)
	}

	wg.Wait()

	// 3. Parse selected dependency files once to extract definitions and their own references.
	definitionIndex := make(map[string]string)
	for symbol, path := range symbolToDefPath {
		definitionIndex[symbol] = path
	}
	for _, path := range sortedDependencyKeys(foundDeps) {
		s.enrichDependencyMetadata(ctx, path, foundDeps[path], definitionIndex)
	}

	result := make(map[string]string)
	totalContextSize := 0

	for _, path := range sortedDependencyKeys(foundDeps) {
		dep := foundDeps[path]
		summary := buildDependencySummary(path, dep, definitionIndex)
		if summary == "" {
			continue
		}
		added := len(summary)
		if totalContextSize+added > MaxDepContextChars {
			continue
		}
		result[path] = summary
		totalContextSize += added
	}

	contextGraph := buildContextGraphSummary(changedFiles, refsByChangedFile, foundDeps, definitionIndex)
	if contextGraph != "" {
		if totalContextSize+len(contextGraph) <= MaxDepContextChars {
			result["_codegraph/context_graph"] = contextGraph
			totalContextSize += len(contextGraph)
		}
	}

	fmt.Printf("✅ [CodeGraph] Analysis complete. Found summaries for %d files (%d chars of context).\n", len(result), totalContextSize)
	if len(result) > 0 {
		fmt.Println("📂 [CodeGraph] Output: Added the following files to context:")
		for _, path := range sortedMapKeys(result) {
			fmt.Printf("  + %s\n", path)
		}
	}
	return result, nil
}

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

func sortedSymbolSlice(symbols map[string]struct{}) []string {
	out := make([]string, 0, len(symbols))
	for s := range symbols {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func graphNodeLabel(path string) string {
	clean := filepath.ToSlash(strings.TrimSpace(path))
	if clean == "" {
		return "unknown"
	}
	clean = strings.TrimSuffix(clean, filepath.Ext(clean))
	parts := strings.Split(clean, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	return clean
}

func relationForReferenceKind(kind string) string {
	switch kind {
	case "call", "method_call":
		return "calls"
	case "type_ref", "constructor_call":
		return "uses_type"
	default:
		return "depends_on"
	}
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

func inferBehaviorSignals(snippets []string) []string {
	combined := strings.ToLower(strings.Join(snippets, "\n"))
	if combined == "" {
		return nil
	}

	type signalRule struct {
		Message  string
		Patterns []string
	}
	rules := []signalRule{
		{
			Message:  "Performs data-store operations",
			Patterns: []string{"select ", "insert ", "update ", "delete ", "query", "sql", "database", "sqlite", "mongodb", "redis"},
		},
		{
			Message:  "Performs network/API operations",
			Patterns: []string{"http.", "fetch(", "axios", "requests.", "grpc", "webhook", "socket", "client.do"},
		},
		{
			Message:  "Touches filesystem I/O",
			Patterns: []string{"readfile", "writefile", "open(", "filepath", "os.", "ioutil", "fs.", "file."},
		},
		{
			Message:  "Handles identity/authentication data",
			Patterns: []string{"jwt", "oauth", "token", "session", "auth", "password", "secret", "credential"},
		},
		{
			Message:  "Serializes or parses structured payloads",
			Patterns: []string{"json", "yaml", "xml", "marshal", "unmarshal", "encode", "decode", "parse"},
		},
		{
			Message:  "Contains concurrency/async coordination",
			Patterns: []string{"goroutine", "mutex", "thread", "async", "await", "promise", "channel", "lock"},
		},
		{
			Message:  "Spawns external commands/processes",
			Patterns: []string{"exec(", "command", "spawn", "system(", "subprocess"},
		},
	}

	var signals []string
	for _, rule := range rules {
		for _, pattern := range rule.Patterns {
			if strings.Contains(combined, pattern) {
				signals = append(signals, rule.Message)
				break
			}
		}
	}
	signals = uniqueStrings(signals)
	if len(signals) > MaxBehaviorSignals {
		signals = signals[:MaxBehaviorSignals]
	}
	return signals
}

func collectDownstreamDependencies(path string, dep *dependencyInfo, definitionIndex map[string]string) []string {
	targets := make(map[string]struct{})
	for _, ref := range dep.References {
		targetPath, ok := definitionIndex[ref.Symbol]
		if !ok || targetPath == path {
			continue
		}
		targets[targetPath] = struct{}{}
	}
	return sortedSymbolSlice(targets)
}

func buildDependencySummary(path string, dep *dependencyInfo, definitionIndex map[string]string) string {
	if dep == nil {
		return ""
	}

	resolvedSymbols := sortedSymbolSlice(dep.Symbols)
	if len(resolvedSymbols) == 0 {
		return ""
	}
	definedSymbols := sortedSymbolSlice(dep.Definitions)
	downstreamDeps := collectDownstreamDependencies(path, dep, definitionIndex)
	signals := inferBehaviorSignals(dep.Snippets)

	var b strings.Builder
	b.WriteString("DEPENDENCY SUMMARY")
	b.WriteString("\n")
	b.WriteString("- File: ")
	b.WriteString(path)
	b.WriteString("\n")
	b.WriteString("- Resolves symbols: ")
	b.WriteString(displayList(resolvedSymbols, MaxSymbolsPerSummary))
	b.WriteString("\n")

	if len(definedSymbols) > 0 {
		b.WriteString("- Local definitions observed: ")
		b.WriteString(displayList(definedSymbols, MaxSymbolsPerSummary))
		b.WriteString("\n")
	}
	if len(downstreamDeps) > 0 {
		nodeLabels := make([]string, 0, len(downstreamDeps))
		for _, depPath := range downstreamDeps {
			nodeLabels = append(nodeLabels, graphNodeLabel(depPath))
		}
		b.WriteString("- Depends on components: ")
		b.WriteString(displayList(nodeLabels, MaxSymbolsPerSummary))
		b.WriteString("\n")
	}
	if len(signals) > 0 {
		b.WriteString("- Behavior signals: ")
		b.WriteString(strings.Join(signals, "; "))
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

type edgeKey struct {
	From     string
	To       string
	Relation string
}

func sortedEdgeKeys(edges map[edgeKey]map[string]struct{}) []edgeKey {
	keys := make([]edgeKey, 0, len(edges))
	for key := range edges {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].From == keys[j].From {
			if keys[i].To == keys[j].To {
				return keys[i].Relation < keys[j].Relation
			}
			return keys[i].To < keys[j].To
		}
		return keys[i].From < keys[j].From
	})
	return keys
}

func countGraphNodes(edges map[edgeKey]map[string]struct{}) int {
	nodes := make(map[string]struct{})
	for key := range edges {
		nodes[key.From] = struct{}{}
		nodes[key.To] = struct{}{}
	}
	return len(nodes)
}

func buildDataFlowLines(changedFiles map[string]string, edges map[edgeKey]map[string]struct{}) []string {
	changedSet := make(map[string]struct{}, len(changedFiles))
	for path := range changedFiles {
		changedSet[path] = struct{}{}
	}

	edgeKeys := sortedEdgeKeys(edges)
	lines := make([]string, 0, MaxDataFlowLines)
	seen := make(map[string]struct{})

	// Direct flow from changed files
	for _, key := range edgeKeys {
		if _, ok := changedSet[key.From]; !ok {
			continue
		}
		symbols := sortedSymbolSlice(edges[key])
		line := fmt.Sprintf("%s -> %s via %s", graphNodeLabel(key.From), graphNodeLabel(key.To), displayList(symbols, 2))
		if _, exists := seen[line]; exists {
			continue
		}
		seen[line] = struct{}{}
		lines = append(lines, line)
		if len(lines) >= MaxDataFlowLines {
			return lines
		}
	}

	// One-hop extension to expose chain-like flow
	for _, first := range edgeKeys {
		if _, ok := changedSet[first.From]; !ok {
			continue
		}
		for _, second := range edgeKeys {
			if first.To != second.From {
				continue
			}
			line := fmt.Sprintf("%s -> %s -> %s", graphNodeLabel(first.From), graphNodeLabel(first.To), graphNodeLabel(second.To))
			if _, exists := seen[line]; exists {
				continue
			}
			seen[line] = struct{}{}
			lines = append(lines, line)
			if len(lines) >= MaxDataFlowLines {
				return lines
			}
		}
	}

	return lines
}

func buildContextGraphSummary(changedFiles map[string]string, refsByChangedFile map[string][]Reference, foundDeps map[string]*dependencyInfo, definitionIndex map[string]string) string {
	edges := make(map[edgeKey]map[string]struct{})

	addEdge := func(fromPath, toPath, relation, symbol string) {
		if fromPath == "" || toPath == "" || fromPath == toPath {
			return
		}
		key := edgeKey{
			From:     fromPath,
			To:       toPath,
			Relation: relation,
		}
		if edges[key] == nil {
			edges[key] = make(map[string]struct{})
		}
		if symbol != "" {
			edges[key][symbol] = struct{}{}
		}
	}

	// Changed file -> dependency edges
	for changedPath, refs := range refsByChangedFile {
		for _, ref := range refs {
			targetPath, ok := definitionIndex[ref.Symbol]
			if !ok {
				continue
			}
			addEdge(changedPath, targetPath, relationForReferenceKind(ref.Kind), ref.Symbol)
		}
	}

	// Dependency -> dependency edges
	for depPath, dep := range foundDeps {
		for _, ref := range dep.References {
			targetPath, ok := definitionIndex[ref.Symbol]
			if !ok {
				continue
			}
			addEdge(depPath, targetPath, relationForReferenceKind(ref.Kind), ref.Symbol)
		}
	}

	if len(edges) == 0 {
		return ""
	}

	edgeKeys := sortedEdgeKeys(edges)
	var b strings.Builder
	b.WriteString("CONTEXT GRAPH\n")
	b.WriteString(fmt.Sprintf("- Nodes: %d | Edges: %d\n", countGraphNodes(edges), len(edgeKeys)))
	for _, key := range edgeKeys {
		edgeSymbols := sortedSymbolSlice(edges[key])
		b.WriteString(fmt.Sprintf("[%s] --%s--> [%s] (via: %s)\n",
			graphNodeLabel(key.From), key.Relation, graphNodeLabel(key.To), displayList(edgeSymbols, MaxGraphEdgeSymbols)))
	}

	dataFlowLines := buildDataFlowLines(changedFiles, edges)
	if len(dataFlowLines) > 0 {
		b.WriteString("\nDATA FLOW\n")
		for _, line := range dataFlowLines {
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return strings.TrimSpace(b.String())
}

func (s *Service) enrichDependencyMetadata(ctx context.Context, path string, dep *dependencyInfo, definitionIndex map[string]string) {
	if dep == nil {
		return
	}
	if dep.Definitions == nil {
		dep.Definitions = make(map[string]struct{})
	}

	fullPath := filepath.Join(s.RepoPath, path)
	contentBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return
	}

	langConfig, ok := GetLanguageForFile(path)
	if !ok {
		return
	}

	root, err := s.Parser.ParseFile(ctx, contentBytes, langConfig)
	if err != nil {
		return
	}

	definitions, err := s.Parser.ExtractDefinitions(root, contentBytes, strings.ToLower(langConfig.Name))
	if err == nil {
		for _, def := range definitions {
			if def == "" || isBuiltinSymbol(def) {
				continue
			}
			dep.Definitions[def] = struct{}{}
			if _, exists := definitionIndex[def]; !exists {
				definitionIndex[def] = path
			}
		}
	}

	references, err := s.Parser.ExtractReferenceDetails(root, contentBytes, strings.ToLower(langConfig.Name))
	if err == nil {
		for _, ref := range references {
			if ref.Symbol == "" || isBuiltinSymbol(ref.Symbol) {
				continue
			}
			dep.References = addReferenceUnique(dep.References, ref)
		}
	}
}
