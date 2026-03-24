package codegraph

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type RuntimeGraphView struct {
	ChangedSymbols []Symbol
	Symbols        map[string]Symbol
	Callers        map[string][]SymbolEdge
	Callees        map[string][]SymbolEdge
	PackageDeps    []PackageDependency
}

var runtimeDiffHeaderRegex = regexp.MustCompile(`^diff --git a/(.*) b/(.*)$`)
var runtimeHunkRegex = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

func (g *Graph) BuildRuntimeGraphView(diff string, changedFiles map[string]string, depth int) *RuntimeGraphView {
	if g == nil {
		return nil
	}
	if depth <= 0 {
		depth = 2
	}
	if len(g.Symbols) == 0 {
		rebuildGraphIndexes(g, "")
	}

	changedLines := extractChangedLines(diff)
	seed := make(map[string]Symbol)

	for path := range changedFiles {
		entry, ok := g.Files[path]
		if !ok {
			continue
		}
		lines := changedLines[path]
		for _, def := range entry.Definitions {
			if def.QualifiedSymbol == "" {
				continue
			}
			if len(lines) == 0 || overlapsChangedLines(def, lines) {
				seed[def.QualifiedSymbol] = symbolFromDefinition(path, def)
			}
		}
	}

	if len(seed) == 0 {
		for path := range changedFiles {
			entry, ok := g.Files[path]
			if !ok {
				continue
			}
			for _, def := range entry.Definitions {
				if def.QualifiedSymbol != "" {
					seed[def.QualifiedSymbol] = symbolFromDefinition(path, def)
				}
			}
		}
	}

	if len(seed) == 0 {
		return nil
	}

	outgoing := make(map[string][]SymbolEdge)
	incoming := make(map[string][]SymbolEdge)
	for _, edge := range g.SymbolEdges {
		outgoing[edge.From] = append(outgoing[edge.From], edge)
		incoming[edge.To] = append(incoming[edge.To], edge)
	}

	view := &RuntimeGraphView{
		Symbols:     make(map[string]Symbol),
		Callers:     make(map[string][]SymbolEdge),
		Callees:     make(map[string][]SymbolEdge),
		PackageDeps: make([]PackageDependency, 0),
	}

	type queueItem struct {
		ID    string
		Depth int
	}
	visited := make(map[string]int)
	queue := make([]queueItem, 0, len(seed))
	for _, sym := range seed {
		view.ChangedSymbols = append(view.ChangedSymbols, sym)
		view.Symbols[sym.QualifiedSymbol] = sym
		visited[sym.QualifiedSymbol] = 0
		queue = append(queue, queueItem{ID: sym.QualifiedSymbol, Depth: 0})
	}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if item.Depth >= depth {
			continue
		}

		for _, edge := range outgoing[item.ID] {
			view.Callees[item.ID] = append(view.Callees[item.ID], edge)
			if sym, ok := g.Symbols[edge.To]; ok {
				view.Symbols[sym.QualifiedSymbol] = sym
				if prevDepth, seen := visited[sym.QualifiedSymbol]; !seen || prevDepth > item.Depth+1 {
					visited[sym.QualifiedSymbol] = item.Depth + 1
					queue = append(queue, queueItem{ID: sym.QualifiedSymbol, Depth: item.Depth + 1})
				}
			}
		}
		for _, edge := range incoming[item.ID] {
			view.Callers[item.ID] = append(view.Callers[item.ID], edge)
			if sym, ok := g.Symbols[edge.From]; ok {
				view.Symbols[sym.QualifiedSymbol] = sym
				if prevDepth, seen := visited[sym.QualifiedSymbol]; !seen || prevDepth > item.Depth+1 {
					visited[sym.QualifiedSymbol] = item.Depth + 1
					queue = append(queue, queueItem{ID: sym.QualifiedSymbol, Depth: item.Depth + 1})
				}
			}
		}
	}

	pkgSeen := make(map[string]struct{})
	for _, dep := range g.PackageDeps {
		if packageUsedInView(dep.From, view.Symbols) || packageUsedInView(dep.To, view.Symbols) {
			key := dep.From + "\x00" + dep.To
			if _, exists := pkgSeen[key]; exists {
				continue
			}
			pkgSeen[key] = struct{}{}
			view.PackageDeps = append(view.PackageDeps, dep)
		}
	}

	sort.Slice(view.ChangedSymbols, func(i, j int) bool {
		if view.ChangedSymbols[i].File == view.ChangedSymbols[j].File {
			return view.ChangedSymbols[i].Line < view.ChangedSymbols[j].Line
		}
		return view.ChangedSymbols[i].File < view.ChangedSymbols[j].File
	})

	return view
}

func FormatRuntimeGraphView(view *RuntimeGraphView) string {
	if view == nil || len(view.Symbols) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("RUNTIME REVIEW SLICE\n")
	b.WriteString(fmt.Sprintf("- Changed symbols: %d\n", len(view.ChangedSymbols)))
	b.WriteString(fmt.Sprintf("- Slice symbols: %d\n", len(view.Symbols)))

	if len(view.ChangedSymbols) > 0 {
		b.WriteString("CHANGED SYMBOLS\n")
		for _, sym := range view.ChangedSymbols {
			b.WriteString(fmt.Sprintf("- %s (%s:%d)\n", sym.QualifiedSymbol, sym.File, sym.Line))
		}
	}

	callees := sortedRuntimeEdges(view.Callees)
	if len(callees) > 0 {
		b.WriteString("CALLEES\n")
		for _, edge := range callees {
			b.WriteString(fmt.Sprintf("- %s -> %s via %s\n", edge.From, edge.To, edge.Symbol))
		}
	}

	// NOTE: CALLERS are intentionally omitted from the static context.
	// Agents can use the 'get_callers' tool on-demand to check callers
	// for specific symbols, which is more targeted and saves ~4K tokens.

	if len(view.PackageDeps) > 0 {
		sort.Slice(view.PackageDeps, func(i, j int) bool {
			if view.PackageDeps[i].From == view.PackageDeps[j].From {
				return view.PackageDeps[i].To < view.PackageDeps[j].To
			}
			return view.PackageDeps[i].From < view.PackageDeps[j].From
		})
		b.WriteString("PACKAGE DEPENDENCIES\n")
		for _, dep := range view.PackageDeps {
			if dep.Via != "" {
				b.WriteString(fmt.Sprintf("- %s -> %s (via %s)\n", dep.From, dep.To, dep.Via))
			} else {
				b.WriteString(fmt.Sprintf("- %s -> %s\n", dep.From, dep.To))
			}
		}
	}

	return strings.TrimSpace(b.String())
}

func symbolFromDefinition(path string, def Definition) Symbol {
	return Symbol{
		QualifiedSymbol: def.QualifiedSymbol,
		Symbol:          def.Symbol,
		Package:         def.Package,
		File:            path,
		Owner:           def.Owner,
		Kind:            def.Kind,
		Line:            def.Line,
		EndLine:         def.EndLine,
	}
}

func overlapsChangedLines(def Definition, lines map[int]struct{}) bool {
	for line := range lines {
		if line >= def.Line && (def.EndLine == 0 || line <= def.EndLine) {
			return true
		}
	}
	return false
}

func packageUsedInView(pkg string, symbols map[string]Symbol) bool {
	for _, sym := range symbols {
		if sym.Package == pkg {
			return true
		}
	}
	return false
}

func sortedRuntimeEdges(m map[string][]SymbolEdge) []SymbolEdge {
	var out []SymbolEdge
	for _, edges := range m {
		out = append(out, edges...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From == out[j].From {
			if out[i].To == out[j].To {
				return out[i].Symbol < out[j].Symbol
			}
			return out[i].To < out[j].To
		}
		return out[i].From < out[j].From
	})
	return out
}

func extractChangedLines(diff string) map[string]map[int]struct{} {
	result := make(map[string]map[int]struct{})
	// Normalize line endings for Windows compatibility
	diff = strings.ReplaceAll(diff, "\r", "")
	lines := strings.Split(diff, "\n")
	var currentFile string
	currentLine := 0

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			matches := runtimeDiffHeaderRegex.FindStringSubmatch(line)
			if len(matches) >= 3 {
				currentFile = strings.Trim(matches[2], "\"")
			}
			continue
		}
		if matches := runtimeHunkRegex.FindStringSubmatch(line); len(matches) >= 2 {
			fmt.Sscanf(matches[1], "%d", &currentLine)
			continue
		}
		if currentFile == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			if result[currentFile] == nil {
				result[currentFile] = make(map[int]struct{})
			}
			result[currentFile][currentLine] = struct{}{}
			currentLine++
		case strings.HasPrefix(line, "-"):
			continue
		default:
			currentLine++
		}
	}

	return result
}
