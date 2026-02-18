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

// MaxSnippetCharsPerFile caps total snippet content per dependency to stay within context budget.
const MaxSnippetCharsPerFile = 2000

// buildDependencySummary creates a compact LLM-readable summary for a single dependency file.
// It includes actual code snippets (not just symbol names) so the LLM can reason about implementations.
func buildDependencySummary(path string, dep *dependencyInfo, definitionIndex map[string]string) string {
	if dep == nil {
		return ""
	}

	resolvedSymbols := sortedSymbolSlice(dep.Symbols)
	if len(resolvedSymbols) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("DEPENDENCY: ")
	b.WriteString(path)
	b.WriteString(" (resolves: ")
	b.WriteString(displayList(resolvedSymbols, MaxSymbolsPerSummary))
	b.WriteString(")\n")

	// Include actual code snippets — this is what makes context useful
	totalSnippetChars := 0
	for _, snippet := range dep.Snippets {
		trimmed := strings.TrimSpace(snippet)
		if trimmed == "" {
			continue
		}
		// Truncate individual snippets that are too long
		if len(trimmed) > 500 {
			trimmed = trimmed[:497] + "..."
		}
		if totalSnippetChars+len(trimmed) > MaxSnippetCharsPerFile {
			b.WriteString("// ... (remaining snippets omitted for context budget)\n")
			break
		}
		b.WriteString(trimmed)
		b.WriteString("\n\n")
		totalSnippetChars += len(trimmed)
	}

	// Downstream dependencies — the LLM benefits from knowing the chain
	downstreamDeps := collectDownstreamDependencies(path, dep, definitionIndex)
	if len(downstreamDeps) > 0 {
		nodeLabels := make([]string, 0, len(downstreamDeps))
		for _, depPath := range downstreamDeps {
			nodeLabels = append(nodeLabels, graphNodeLabel(depPath))
		}
		b.WriteString("// depends on: ")
		b.WriteString(displayList(nodeLabels, MaxSymbolsPerSummary))
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
