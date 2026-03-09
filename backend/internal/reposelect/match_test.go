package reposelect

import (
	"strings"
	"testing"

	"code-review/backend/internal/codegraph"
)

func TestExtractLocalSignals(t *testing.T) {
	localGraph := &codegraph.Graph{
		Files: map[string]codegraph.FileEntry{
			"auth/client.go": {
				Language: "go",
				Definitions: []codegraph.Definition{
					{Symbol: "ValidateToken", Kind: "function"},
					{Symbol: "internalCheck", Kind: "function"},
				},
				Imports: []string{"fmt", "github.com/google/go-github/v60/github"},
			},
		},
	}

	changedFiles := map[string]string{
		"auth/client.go": "+ func ValidateToken() {}",
	}

	signals := ExtractLocalSignals(localGraph, changedFiles)

	if signals.ChangedExports["ValidateToken"] == "" {
		t.Error(" expected ValidateToken to be in ChangedExports")
	}
	if signals.ChangedExports["internalCheck"] != "" {
		t.Error(" expected internalCheck NOT to be in ChangedExports (lowercase)")
	}
	if signals.ChangedImports["fmt"] == "" {
		t.Error(" expected fmt to be in ChangedImports")
	}
}

func TestExtractRemoteFacts(t *testing.T) {
	remoteGraph := &codegraph.RemoteRepoGraph{
		RepoFullName: "org/service-a",
		Files: map[string]codegraph.RemoteFileEntry{
			"main.go": {
				Language: "go",
				Definitions: []codegraph.Definition{
					{Symbol: "ServiceAHandler", Kind: "function"},
				},
				Imports: []string{"fmt"},
			},
		},
	}

	facts := ExtractRemoteFacts(remoteGraph)

	if len(facts.Exports) != 1 || facts.Exports["ServiceAHandler"] == "" {
		t.Errorf("unexpected exports: %v", facts.Exports)
	}

	if len(facts.Imports) != 1 || facts.Imports["fmt"] == "" {
		t.Errorf("unexpected imports: %v", facts.Imports)
	}
}

func TestMatchRepos(t *testing.T) {
	signals := LocalSignals{
		ChangedExports: map[string]string{"ValidateToken": "go"},
		ChangedImports: map[string]string{"github.com/google/uuid": "go"},
	}

	remoteGraphs := map[string]*codegraph.RemoteRepoGraph{
		"org/consumer-repo": {
			RepoFullName: "org/consumer-repo",
			Files: map[string]codegraph.RemoteFileEntry{
				"main.go": {
					Language: "go",
					Imports:  []string{"github.com/google/uuid"},
					Definitions: []codegraph.Definition{
						{Symbol: "UnrelatedSymbol"},
					},
				},
			},
		},
		"org/consumer-symbol": {
			RepoFullName: "org/consumer-symbol",
			Files: map[string]codegraph.RemoteFileEntry{
				"main.go": {
					Language: "go",
					Definitions: []codegraph.Definition{
						{Symbol: "ValidateToken"},
					},
				},
			},
		},
	}

	candidates := MatchRepos(signals, remoteGraphs)

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	var foundImport, foundSymbol bool
	for _, c := range candidates {
		if strings.Contains(c.Reason, "Shared import context match") {
			foundImport = true
		}
		if strings.Contains(c.Reason, "Shared symbol match") {
			foundSymbol = true
		}
	}

	if !foundImport || !foundSymbol {
		t.Errorf("expected both import and symbol matches, got reasons: %v", candidates)
	}
}
