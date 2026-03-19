package codegraph

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestBuildSlimDependencyIndex_WithCodeGraphEntries(t *testing.T) {
	fullDeps := map[string]string{
		"auth/service.go":          "DEPENDENCY: auth/service.go (resolves: ValidateToken, CreateUser)\nfunc ValidateToken() {}\nfunc CreateUser() {}",
		"_codegraph/runtime_slice": "RUNTIME REVIEW SLICE\n- Changed symbols: 1\n- Slice symbols: 5",
		"_codegraph/context_graph": "CONTEXT GRAPH\n[pkg.a] --calls--> [pkg.b]",
	}

	result := BuildSlimDependencyIndex(fullDeps)

	assert.NotNil(t, result)
	assert.Contains(t, result, "_codegraph/runtime_slice")
	assert.Contains(t, result, "_codegraph/context_graph")
	assert.Contains(t, result, "_codegraph/dependency_index")

	index := result["_codegraph/dependency_index"]
	assert.Contains(t, index, "auth/service.go: ValidateToken, CreateUser")

	// Verify that internal entries are NOT in the slim index text itself, just in the map
	assert.NotContains(t, index, "_codegraph/runtime_slice")
}

func TestBuildSlimDependencyIndex_OnlyCodeGraphEntries(t *testing.T) {
	fullDeps := map[string]string{
		"_codegraph/runtime_slice": "RUNTIME REVIEW SLICE\n- Changed symbols: 1",
	}

	result := BuildSlimDependencyIndex(fullDeps)

	assert.NotNil(t, result)
	assert.Contains(t, result, "_codegraph/runtime_slice")
	// Should NOT have a dependency_index if no file-level deps existed
	assert.NotContains(t, result, "_codegraph/dependency_index")
}
