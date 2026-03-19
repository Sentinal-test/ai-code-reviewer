package codegraph

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// edgeKey represents a directed relationship between two files.
type edgeKey struct {
	From     string
	To       string
	Relation string
}

// graphNodeLabel converts a file path into a compact two-level label (e.g. "api.handler").
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

// relationForReferenceKind maps AST reference kinds to human-readable edge labels.
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

// buildContextGraphSummary builds the CONTEXT GRAPH + DATA FLOW text block for the LLM.
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

// buildDataFlowLines traces data flow chains starting from changed files.
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

// sortedEdgeKeys returns edge keys in deterministic order.
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

// countGraphNodes counts unique nodes in the edge set.
func countGraphNodes(edges map[edgeKey]map[string]struct{}) int {
	nodes := make(map[string]struct{})
	for key := range edges {
		nodes[key.From] = struct{}{}
		nodes[key.To] = struct{}{}
	}
	return len(nodes)
}