package chunker

import (
	"code-review/backend/internal/codegraph"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEstimateTokens(t *testing.T) {
	assert.Equal(t, 0, EstimateTokens(""))
	assert.Equal(t, 2, EstimateTokens("12345678"))                  // 8/4 = 2
	assert.Equal(t, 250, EstimateTokens(strings.Repeat("x", 1000))) // 1000/4 = 250
}

func TestGroupFiles_SingleChunk(t *testing.T) {
	// Small PR — everything fits in one chunk
	files := map[string]string{
		"main.go":    strings.Repeat("x", 100),
		"handler.go": strings.Repeat("x", 100),
	}
	chunks := GroupFiles(files, "diff content", nil, 1000)

	assert.Equal(t, 1, len(chunks))
	assert.Equal(t, 2, len(chunks[0].Files))
	assert.Equal(t, 1, chunks[0].Index)
	assert.Equal(t, 1, chunks[0].Total)
}

func TestGroupFiles_MultipleChunks(t *testing.T) {
	// Create files that exceed the budget
	files := map[string]string{
		"pkg/a/file1.go": strings.Repeat("a", 400),
		"pkg/a/file2.go": strings.Repeat("b", 400),
		"pkg/b/file3.go": strings.Repeat("c", 400),
		"pkg/b/file4.go": strings.Repeat("d", 400),
	}

	// Budget = 250 tokens = 1000 chars → each dir pair is 800 chars (200 tokens)
	chunks := GroupFiles(files, "", nil, 250)

	assert.True(t, len(chunks) >= 2, "Expected at least 2 chunks, got %d", len(chunks))

	// All files should be present across chunks
	totalFiles := 0
	for _, c := range chunks {
		totalFiles += len(c.Files)
	}
	assert.Equal(t, 4, totalFiles)

	// Chunks should be numbered correctly
	for i, c := range chunks {
		assert.Equal(t, i+1, c.Index)
		assert.Equal(t, len(chunks), c.Total)
	}
}

func TestGroupFiles_GraphAware(t *testing.T) {
	// Files in different directories but connected by graph edges
	files := map[string]string{
		"pkg/auth/token.go":     strings.Repeat("x", 600),
		"pkg/api/middleware.go": strings.Repeat("y", 600),
		"pkg/db/repo.go":        strings.Repeat("z", 600),
	}

	edges := []codegraph.Edge{
		{From: "pkg/api/middleware.go", To: "pkg/auth/token.go", Symbol: "ValidateToken", Relation: "calls"},
	}

	// Total: 1800 chars = 450 tokens.
	// Budget: 350 tokens. Connected pair (token+middleware) = 300 tokens → fits in one chunk.
	// repo.go alone = 150 tokens → second chunk.
	chunks := GroupFiles(files, "", edges, 350)

	// token.go and middleware.go should be in the same chunk (connected by edge + graph union)
	// repo.go should be in a separate chunk
	assert.Equal(t, 2, len(chunks), "Expected 2 chunks, got %d", len(chunks))

	// Find which chunk has token.go
	var tokenChunk, repoChunk Chunk
	for _, c := range chunks {
		if _, ok := c.Files["pkg/auth/token.go"]; ok {
			tokenChunk = c
		}
		if _, ok := c.Files["pkg/db/repo.go"]; ok {
			repoChunk = c
		}
	}

	// token.go and middleware.go should be together
	assert.Contains(t, tokenChunk.Files, "pkg/auth/token.go")
	assert.Contains(t, tokenChunk.Files, "pkg/api/middleware.go")

	// repo.go should be alone
	assert.Contains(t, repoChunk.Files, "pkg/db/repo.go")
	assert.Equal(t, 1, len(repoChunk.Files))
}

func TestGroupFiles_CrossRefs(t *testing.T) {
	// When files are split across chunks, cross-refs should be populated
	files := map[string]string{
		"pkg/a/file1.go": strings.Repeat("x", 800),
		"pkg/b/file2.go": strings.Repeat("y", 800),
	}

	edges := []codegraph.Edge{
		{From: "pkg/a/file1.go", To: "pkg/b/file2.go", Symbol: "DoThing", Relation: "calls"},
	}

	// Budget forces them into separate chunks (each file is 200 tokens)
	chunks := GroupFiles(files, "", edges, 150)

	if len(chunks) >= 2 {
		// At least one chunk should have cross-refs
		hasCrossRefs := false
		for _, c := range chunks {
			if len(c.CrossRefs) > 0 {
				hasCrossRefs = true
			}
		}
		assert.True(t, hasCrossRefs, "Expected cross-refs when connected files are split")
	}
}

func TestSplitDiffByFile(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
index abc..def 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
diff --git a/handler.go b/handler.go
index abc..def 100644
--- a/handler.go
+++ b/handler.go
@@ -10,3 +10,4 @@
 func handle() {
+    fmt.Println("hello")
 }
`
	sections := splitDiffByFile(diff)

	assert.Equal(t, 2, len(sections))
	assert.Contains(t, sections, "main.go")
	assert.Contains(t, sections, "handler.go")
	assert.Contains(t, sections["main.go"], "package main")
	assert.Contains(t, sections["handler.go"], "func handle()")
}

func TestGroupFiles_NilEdges(t *testing.T) {
	// Should work fine with nil edges (directory-only grouping fallback)
	files := map[string]string{
		"pkg/a/f1.go": strings.Repeat("x", 400),
		"pkg/b/f2.go": strings.Repeat("y", 400),
	}
	chunks := GroupFiles(files, "", nil, 150)
	assert.True(t, len(chunks) >= 1)

	totalFiles := 0
	for _, c := range chunks {
		totalFiles += len(c.Files)
	}
	assert.Equal(t, 2, totalFiles)
}

func TestGroupFiles_OversizedComponent(t *testing.T) {
	// A single component exceeds budget — should get its own chunk
	files := map[string]string{
		"big/file1.go": strings.Repeat("x", 2000),
		"big/file2.go": strings.Repeat("y", 2000),
		"small/f.go":   strings.Repeat("z", 100),
	}

	chunks := GroupFiles(files, "", nil, 200)

	// big files should be in their own chunk, small file in another
	assert.True(t, len(chunks) >= 2, fmt.Sprintf("Expected >= 2 chunks, got %d", len(chunks)))
}
