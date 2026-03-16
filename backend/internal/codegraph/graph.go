package codegraph

import "time"

// Graph is the serializable code graph index for a repository.
// It stores definitions, references, and cross-file edges extracted via tree-sitter AST parsing.
type Graph struct {
	Version     int                  `json:"version"`    // schema version (currently 1)
	IndexedAt   time.Time            `json:"indexed_at"` // when the graph was last built/updated
	CommitSHA   string               `json:"commit_sha"` // git commit this graph was built from
	Files       map[string]FileEntry `json:"files"`      // path -> file entry
	Edges       []Edge               `json:"edges"`      // cross-file relationships
	Symbols     map[string]Symbol    `json:"symbols,omitempty"`
	SymbolEdges []SymbolEdge         `json:"symbol_edges,omitempty"`
	PackageDeps []PackageDependency  `json:"package_deps,omitempty"`

	// InvertedReferences is a non-serializable index: symbol -> list of calling locations.
	// Populated during rebuildGraphIndexes to speed up get_callers.
	InvertedReferences map[string][]SymbolLocation `json:"-"`
}

// SymbolLocation represents a specific place where a symbol is referenced.
type SymbolLocation struct {
	File           string `json:"file"`
	Line           int    `json:"line"`
	ScopeQualified string `json:"scope_qualified"`
	Kind           string `json:"kind"`
}

// FileEntry stores parsed metadata for a single source file.
type FileEntry struct {
	Hash          string            `json:"hash"`                     // SHA-256 of file content
	Language      string            `json:"language"`                 // "go", "python", "javascript", etc.
	Package       string            `json:"package,omitempty"`        // package / module namespace
	Definitions   []Definition      `json:"definitions"`              // symbols defined in this file
	References    []Reference       `json:"references"`               // symbols referenced from this file
	Imports       []string          `json:"imports,omitempty"`        // import paths (legacy)
	ImportAliases map[string]string `json:"import_aliases,omitempty"` // alias -> import path
}

// Definition represents a symbol defined in a source file.
type Definition struct {
	Symbol          string `json:"symbol"`                     // e.g. "ProcessPR", "AuthService"
	QualifiedSymbol string `json:"qualified_symbol,omitempty"` // e.g. "llm.ToolExecutor.Execute"
	Package         string `json:"package,omitempty"`          // package / module namespace
	Owner           string `json:"owner,omitempty"`            // receiver / class / enclosing type
	Kind            string `json:"kind"`                       // "function", "method", "type", "class", "interface"
	Line            int    `json:"line"`                       // 1-indexed start line number
	EndLine         int    `json:"end_line,omitempty"`         // 1-indexed end line number
}

// Edge represents a cross-file relationship between two files.
type Edge struct {
	From     string `json:"from"`     // source file path
	To       string `json:"to"`       // target file path
	Symbol   string `json:"symbol"`   // symbol that creates the relationship
	Relation string `json:"relation"` // "calls", "uses_type", "depends_on"
}

// Symbol stores a symbol-level node used for runtime review slices.
type Symbol struct {
	QualifiedSymbol string `json:"qualified_symbol"`
	Symbol          string `json:"symbol"`
	Package         string `json:"package,omitempty"`
	File            string `json:"file"`
	Owner           string `json:"owner,omitempty"`
	Kind            string `json:"kind"`
	Line            int    `json:"line"`
	EndLine         int    `json:"end_line,omitempty"`
}

// SymbolEdge stores a symbol-to-symbol relationship.
type SymbolEdge struct {
	From     string `json:"from"`      // source qualified symbol
	To       string `json:"to"`        // destination qualified symbol
	Symbol   string `json:"symbol"`    // unresolved/raw symbol text
	FromFile string `json:"from_file"` // source file path
	ToFile   string `json:"to_file"`   // destination file path
	Relation string `json:"relation"`  // "calls", "uses_type", "depends_on"
}

// PackageDependency stores package/module level coupling for architecture checks.
type PackageDependency struct {
	From string `json:"from"`
	To   string `json:"to"`
	Via  string `json:"via,omitempty"`
}

// NewGraph creates a new empty graph with the current schema version.
func NewGraph() *Graph {
	return &Graph{
		Version: 4,
		Files:   make(map[string]FileEntry),
		Symbols: make(map[string]Symbol),
	}
}

// RemoveFile removes all entries for a file from the graph.
func (g *Graph) RemoveFile(path string) {
	delete(g.Files, path)
	// Remove edges involving this file
	filtered := make([]Edge, 0, len(g.Edges))
	for _, e := range g.Edges {
		if e.From != path && e.To != path {
			filtered = append(filtered, e)
		}
	}
	g.Edges = filtered

	filteredSymbolEdges := make([]SymbolEdge, 0, len(g.SymbolEdges))
	for _, e := range g.SymbolEdges {
		if e.FromFile != path && e.ToFile != path {
			filteredSymbolEdges = append(filteredSymbolEdges, e)
		}
	}
	g.SymbolEdges = filteredSymbolEdges

	if len(g.Symbols) > 0 {
		for key, sym := range g.Symbols {
			if sym.File == path {
				delete(g.Symbols, key)
			}
		}
	}
}

// FileCount returns the number of indexed files.
func (g *Graph) FileCount() int {
	return len(g.Files)
}

// DefinitionCount returns the total number of definitions across all files.
func (g *Graph) DefinitionCount() int {
	count := 0
	for _, f := range g.Files {
		count += len(f.Definitions)
	}
	return count
}

// ReferenceCount returns the total number of references across all files.
func (g *Graph) ReferenceCount() int {
	count := 0
	for _, f := range g.Files {
		count += len(f.References)
	}
	return count
}

// FindDefinitions returns all definitions matching a given symbol name.
// Used to handle ambiguity in tool calls.
func (g *Graph) FindDefinitions(symbol string) map[string]*Definition {
	results := make(map[string]*Definition)
	if g == nil {
		return results
	}

	for path, entry := range g.Files {
		for i := range entry.Definitions {
			def := &entry.Definitions[i]
			if def.Symbol == symbol || def.QualifiedSymbol == symbol {
				results[path+":"+def.Symbol] = def
			}
		}
	}
	return results
}

// FindDefinition looks up which file defines a given symbol.
// Returns the file path and definition, or empty string if not found.
func (g *Graph) FindDefinition(symbol string) (string, *Definition) {
	if g == nil {
		return "", nil
	}

	if path, def := g.findDefinitionByQualified(symbol); def != nil {
		return path, def
	}

	var fallbackPath string
	var fallbackDef *Definition
	for path, entry := range g.Files {
		for i := range entry.Definitions {
			def := &entry.Definitions[i]
			if def.Symbol == symbol || def.QualifiedSymbol == symbol {
				if fallbackDef != nil {
					// Ambiguous plain-name lookup.
					return "AMBIGUOUS", nil
				}
				fallbackPath = path
				fallbackDef = def
			}
		}
	}
	return fallbackPath, fallbackDef
}

func (g *Graph) findDefinitionByQualified(symbol string) (string, *Definition) {
	for path, entry := range g.Files {
		for i := range entry.Definitions {
			if entry.Definitions[i].QualifiedSymbol == symbol {
				return path, &entry.Definitions[i]
			}
		}
	}
	return "", nil
}
