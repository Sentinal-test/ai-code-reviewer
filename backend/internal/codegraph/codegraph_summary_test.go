package codegraph

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInferBehaviorSignals_Generic(t *testing.T) {
	signals := inferBehaviorSignals([]string{
		`db.Query("SELECT * FROM users")
resp, _ := http.Get(url)
payload, _ := json.Marshal(req)
token := authToken`,
	})

	assert.Contains(t, signals, "Performs data-store operations")
	assert.Contains(t, signals, "Performs network/API operations")
	assert.Contains(t, signals, "Serializes or parses structured payloads")
	assert.Contains(t, signals, "Handles identity/authentication data")
}

func TestBuildDependencySummary_Generic(t *testing.T) {
	dep := &dependencyInfo{
		Symbols: map[string]struct{}{
			"CreateClient": {},
			"DoRequest":    {},
		},
		Definitions: map[string]struct{}{
			"CreateClient": {},
			"HTTPClient":   {},
		},
		References: []Reference{
			{Symbol: "Config", Kind: "type_ref"},
		},
		Snippets: []string{
			`func CreateClient() *http.Client {
	return &http.Client{}
}`,
			`func DoRequest(url string) (*http.Response, error) {
	client := CreateClient()
	return client.Get(url)
}`,
		},
	}
	definitionIndex := map[string]string{
		"Config": "pkg/config/types.go",
	}

	summary := buildDependencySummary("pkg/net/client.go", dep, definitionIndex)

	assert.Contains(t, summary, "DEPENDENCY: pkg/net/client.go")
	assert.Contains(t, summary, "resolves: CreateClient, DoRequest")
	// Should contain actual code snippets, not just names
	assert.Contains(t, summary, "func CreateClient()")
	assert.Contains(t, summary, "func DoRequest(url string)")
	// Should show downstream dependencies
	assert.Contains(t, summary, "depends on:")
}

func TestBuildContextGraphSummary_Generic(t *testing.T) {
	changedFiles := map[string]string{
		"service/api/handler.go": "package api",
	}
	refsByChangedFile := map[string][]Reference{
		"service/api/handler.go": {
			{Symbol: "AuthService", Kind: "type_ref"},
			{Symbol: "Validate", Kind: "method_call"},
		},
	}
	foundDeps := map[string]*dependencyInfo{
		"service/auth/service.go": {
			Symbols: map[string]struct{}{
				"Validate": {},
			},
			References: []Reference{
				{Symbol: "UserRepo", Kind: "type_ref"},
				{Symbol: "LoadUser", Kind: "call"},
			},
		},
		"service/repo/user_repo.go": {
			Symbols: map[string]struct{}{
				"LoadUser": {},
			},
		},
	}
	definitionIndex := map[string]string{
		"AuthService": "service/auth/service.go",
		"Validate":    "service/auth/service.go",
		"UserRepo":    "service/repo/user_repo.go",
		"LoadUser":    "service/repo/user_repo.go",
	}

	summary := buildContextGraphSummary(changedFiles, refsByChangedFile, foundDeps, definitionIndex)

	assert.True(t, strings.HasPrefix(summary, "CONTEXT GRAPH"))
	assert.Contains(t, summary, "[api.handler] --calls--> [auth.service]")
	assert.Contains(t, summary, "[api.handler] --uses_type--> [auth.service]")
	assert.Contains(t, summary, "[auth.service] --calls--> [repo.user_repo]")
	assert.Contains(t, summary, "[auth.service] --uses_type--> [repo.user_repo]")
	assert.Contains(t, summary, "DATA FLOW")
	assert.Contains(t, summary, "api.handler -> auth.service")
}
