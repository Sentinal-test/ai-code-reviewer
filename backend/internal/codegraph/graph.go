package codegraph

import "time"

// Graph is the serializable code graph index for a repository.
// It stores definitions, references, and cross-file edges extracted via tree-sitter AST parsing.
type Graph struct {
	Version   int                  `json:"version"`    // schema version (currently 1)
	IndexedAt time.Time            `json:"indexed_at"` // when the graph was last built/updated
	CommitSHA string               `json:"commit_sha"` // git commit this graph was built from
	Files     map[string]FileEntry `json:"files"`      // path -> file entry
	Edges     []Edge               `json:"edges"`      // cross-file relationships
}

// FileEntry stores parsed metadata for a single source file.
type FileEntry struct {
	Hash        string       `json:"hash"`              // SHA-256 of file content
	Language    string       `json:"language"`          // "go", "python", "javascript", etc.
	Definitions []Definition `json:"definitions"`       // symbols defined in this file
	References  []Reference  `json:"references"`        // symbols referenced from this file
	Imports     []string     `json:"imports,omitempty"` // import paths (future use)
}

// Definition represents a symbol defined in a source file.
type Definition struct {
	Symbol  string `json:"symbol"`             // e.g. "ProcessPR", "AuthService"
	Kind    string `json:"kind"`               // "function", "method", "type", "class", "interface"
	Line    int    `json:"line"`               // 1-indexed start line number
	EndLine int    `json:"end_line,omitempty"` // 1-indexed end line number
}

// Edge represents a cross-file relationship between two files.
type Edge struct {
	From     string `json:"from"`     // source file path
	To       string `json:"to"`       // target file path
	Symbol   string `json:"symbol"`   // symbol that creates the relationship
	Relation string `json:"relation"` // "calls", "uses_type", "depends_on"
}

// NewGraph creates a new empty graph with the current schema version.
func NewGraph() *Graph {
	return &Graph{
		Version: 2,
		Files:   make(map[string]FileEntry),
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

// FindDefinition looks up which file defines a given symbol.
// Returns the file path and definition, or empty string if not found.
func (g *Graph) FindDefinition(symbol string) (string, *Definition) {
	for path, entry := range g.Files {
		for i, def := range entry.Definitions {
			if def.Symbol == symbol {
				return path, &entry.Definitions[i]
			}
		}
	}
	return "", nil
}
