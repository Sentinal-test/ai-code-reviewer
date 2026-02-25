package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSaveAndLoadGraph(t *testing.T) {
	// Setup
	dir := t.TempDir()
	g := NewGraph()
	g.CommitSHA = "abc1234"
	g.Files["main.go"] = FileEntry{
		Hash:     "deadbeef",
		Language: "go",
		Definitions: []Definition{
			{Symbol: "main", Kind: "function", Line: 5, EndLine: 5},
			{Symbol: "Helper", Kind: "function", Line: 10, EndLine: 10},
		},
		References: []Reference{
			{Symbol: "Helper", Kind: "call"},
		},
	}
	g.Edges = []Edge{
		{From: "main.go", To: "helper.go", Symbol: "Helper", Relation: "calls"},
	}

	// Save
	err := SaveGraph(g, dir)
	assert.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(filepath.Join(dir, "graph.json"))
	assert.NoError(t, err)

	// Load
	loaded, err := LoadGraph(dir)
	assert.NoError(t, err)
	assert.NotNil(t, loaded)

	// Verify round-trip
	assert.Equal(t, g.Version, loaded.Version)
	assert.Equal(t, g.CommitSHA, loaded.CommitSHA)
	assert.Equal(t, len(g.Files), len(loaded.Files))
	assert.Equal(t, g.Files["main.go"].Hash, loaded.Files["main.go"].Hash)
	assert.Equal(t, 2, len(loaded.Files["main.go"].Definitions))
	assert.Equal(t, "main", loaded.Files["main.go"].Definitions[0].Symbol)
	assert.Equal(t, 1, len(loaded.Edges))
}

func TestLoadGraph_Missing(t *testing.T) {
	dir := t.TempDir()
	g, err := LoadGraph(dir)
	assert.NoError(t, err) // Not an error — just a cache miss
	assert.Nil(t, g)
}

func TestGraphRemoveFile(t *testing.T) {
	g := NewGraph()
	g.Files["a.go"] = FileEntry{Hash: "aaa", Language: "go"}
	g.Files["b.go"] = FileEntry{Hash: "bbb", Language: "go"}
	g.Edges = []Edge{
		{From: "a.go", To: "b.go", Symbol: "Foo", Relation: "calls"},
		{From: "b.go", To: "c.go", Symbol: "Bar", Relation: "calls"},
	}

	g.RemoveFile("a.go")

	assert.Equal(t, 1, len(g.Files))
	assert.Equal(t, 1, len(g.Edges)) // only b.go -> c.go remains
	assert.Equal(t, "b.go", g.Edges[0].From)
}

func TestBuildFull(t *testing.T) {
	// Create temp dir with sample files
	dir := t.TempDir()

	// Create a file that defines add
	helperCode := `package main

func add(a, b int) int {
	return a + b
}

type Calculator struct {
	Value int
}
`
	err := os.WriteFile(filepath.Join(dir, "helper.go"), []byte(helperCode), 0644)
	assert.NoError(t, err)

	// Create a file that calls add (cross-file reference)
	mainCode := `package main

func multiply(a, b int) int {
	result := add(a, b)
	return result * 2
}
`
	err = os.WriteFile(filepath.Join(dir, "calc.go"), []byte(mainCode), 0644)
	assert.NoError(t, err)

	// Create a Python file
	pyCode := `def greet(name):
    return f"Hello, {name}"

class Greeter:
    def say_hello(self):
        return greet("World")
`
	err = os.WriteFile(filepath.Join(dir, "greet.py"), []byte(pyCode), 0644)
	assert.NoError(t, err)

	// Init git repo for GetCurrentCommitSHA
	os.MkdirAll(filepath.Join(dir, ".git"), 0755)

	// Build
	g, err := BuildFull(context.Background(), dir)
	assert.NoError(t, err)
	assert.NotNil(t, g)

	// Should have 3 files
	assert.Equal(t, 3, g.FileCount())

	// Check Go definitions in helper.go
	goEntry, ok := g.Files["helper.go"]
	assert.True(t, ok)
	assert.Equal(t, "go", goEntry.Language)
	defNames := make([]string, 0)
	for _, d := range goEntry.Definitions {
		defNames = append(defNames, d.Symbol)
	}
	assert.Contains(t, defNames, "add")
	assert.Contains(t, defNames, "Calculator")

	// Check Python definitions
	pyEntry, ok := g.Files["greet.py"]
	assert.True(t, ok)
	assert.Equal(t, "python", pyEntry.Language)

	// Check there are edges (calc.go's multiply calls helper.go's add)
	assert.True(t, len(g.Edges) > 0, "Expected at least 1 cross-file edge (calc.go -> helper.go via add)")
}

func TestDeltaUpdate(t *testing.T) {
	// Create temp dir
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0755)

	// Create initial files
	file1 := `package main

func original() string {
	return "original"
}
`
	file2 := `package main

func unchanged() int {
	return 42
}
`
	err := os.WriteFile(filepath.Join(dir, "file1.go"), []byte(file1), 0644)
	assert.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "file2.go"), []byte(file2), 0644)
	assert.NoError(t, err)

	// Build full graph
	g, err := BuildFull(context.Background(), dir)
	assert.NoError(t, err)
	assert.Equal(t, 2, g.FileCount())

	// Verify original definition exists
	path, def := g.FindDefinition("original")
	assert.Equal(t, "file1.go", path)
	assert.Equal(t, "function", def.Kind)

	// Modify file1
	file1Modified := `package main

func updated() string {
	return "updated"
}
`
	err = os.WriteFile(filepath.Join(dir, "file1.go"), []byte(file1Modified), 0644)
	assert.NoError(t, err)

	// Delta update
	err = DeltaUpdate(context.Background(), g, dir, []string{"file1.go"})
	assert.NoError(t, err)

	// Verify: file2 should be unchanged
	assert.Equal(t, 2, g.FileCount())
	_, def2 := g.FindDefinition("unchanged")
	assert.NotNil(t, def2)

	// Verify: "original" should be gone, "updated" should exist
	_, defOld := g.FindDefinition("original")
	assert.Nil(t, defOld, "Old definition 'original' should be removed after delta update")
	_, defNew := g.FindDefinition("updated")
	assert.NotNil(t, defNew, "New definition 'updated' should exist after delta update")
}

func TestExtractDefinitionEntries(t *testing.T) {
	p := NewParser()
	ctx := context.Background()

	code := `package main

func add(a, b int) int {
	return a + b
}

type Config struct {
	Host string
	Port int
}

func (c *Config) Validate() error {
	return nil
}
`
	langConfig := SupportedLanguages["go"]
	root, err := p.ParseFile(ctx, []byte(code), &langConfig)
	assert.NoError(t, err)

	defs, err := p.ExtractDefinitionEntries(root, []byte(code), "go")
	assert.NoError(t, err)
	assert.True(t, len(defs) >= 3, "Expected at least 3 definitions (add, Config, Validate)")

	// Check that definitions have correct kinds and non-zero lines
	defMap := make(map[string]Definition)
	for _, d := range defs {
		defMap[d.Symbol] = d
	}

	assert.Equal(t, "function", defMap["add"].Kind)
	assert.True(t, defMap["add"].Line > 0)
	assert.True(t, defMap["add"].EndLine >= defMap["add"].Line, "EndLine should be >= Line")

	assert.Equal(t, "type", defMap["Config"].Kind)
	assert.True(t, defMap["Config"].EndLine > defMap["Config"].Line, "Multi-line type should have EndLine > Line")

	assert.Equal(t, "method", defMap["Validate"].Kind)
	assert.True(t, defMap["Validate"].EndLine >= defMap["Validate"].Line)
}
