package codegraph

import (
	"strings"
)

// dependencyInfo holds parsed metadata about a dependency file.
type dependencyInfo struct {
	Symbols     map[string]struct{}
	Snippets    []string
	Definitions map[string]struct{}
	References  []Reference
}

// buildDependencySummary creates a compact LLM-readable summary for a single dependency file.
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

// collectDownstreamDependencies finds files that a dependency itself depends on.
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

// inferBehaviorSignals detects behavioral patterns from code snippets.
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
