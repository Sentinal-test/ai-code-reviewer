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
				Package:  "auth",
				Definitions: []codegraph.Definition{
					{Symbol: "ValidateToken", QualifiedSymbol: "auth.ValidateToken", Kind: "function"},
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
	if signals.ChangedQualifiedExports["auth.ValidateToken"] == "" {
		t.Error(" expected auth.ValidateToken to be in ChangedQualifiedExports")
	}
	if signals.ChangedPackages["auth"] == "" {
		t.Error(" expected auth package to be in ChangedPackages")
	}
}

func TestExtractRemoteFacts(t *testing.T) {
	remoteGraph := &codegraph.RemoteRepoGraph{
		RepoFullName: "org/service-a",
		Files: map[string]codegraph.RemoteFileEntry{
			"main.go": {
				Language: "go",
				Package:  "servicea",
				Definitions: []codegraph.Definition{
					{Symbol: "ServiceAHandler", QualifiedSymbol: "servicea.ServiceAHandler", Kind: "function"},
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
	if facts.QualifiedExports["servicea.ServiceAHandler"] == "" {
		t.Errorf("unexpected qualified exports: %v", facts.QualifiedExports)
	}
	if facts.Packages["servicea"] == "" {
		t.Errorf("unexpected packages: %v", facts.Packages)
	}
}

func TestMatchRepos(t *testing.T) {
	signals := LocalSignals{
		ProjectName:        "auth-service",
		ChangedExports:     map[string]string{"ValidateToken": "go"},
		ChangedImports:     map[string]string{"github.com/google/uuid": "go"},
		ChangedPackageDirs: map[string]string{"auth": "go"},
	}

	remoteGraphs := map[string]*codegraph.RemoteRepoGraph{
		"org/consumer-uses-export": {
			RepoFullName: "org/consumer-uses-export",
			Files: map[string]codegraph.RemoteFileEntry{
				"main.go": {
					Language:      "go",
					Imports:       []string{"auth-service/auth"},
					ImportAliases: map[string]string{"auth": "auth-service/auth"},
					References: []codegraph.Reference{
						{Symbol: "ValidateToken", Qualifier: "auth", Line: 9},
					},
				},
			},
		},
		"org/consumer-imports-package": {
			RepoFullName: "org/consumer-imports-package",
			Files: map[string]codegraph.RemoteFileEntry{
				"main.go": {
					Language: "go",
					Imports:  []string{"auth-service/auth"},
				},
			},
		},
		"org/consumer-shared-dep-only": {
			RepoFullName: "org/consumer-shared-dep-only",
			Files: map[string]codegraph.RemoteFileEntry{
				"main.go": {
					Language: "go",
					Imports:  []string{"fmt"},
				},
			},
		},
	}

	candidates := MatchRepos(signals, remoteGraphs)

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	var foundConsumer, foundImport bool
	for _, c := range candidates {
		if strings.Contains(c.Reason, "Consumes changed exported symbol") {
			foundConsumer = true
		}
		if strings.Contains(c.Reason, "Imports changed project package") {
			foundImport = true
		}
	}

	if !foundImport || !foundConsumer {
		t.Errorf("expected both import and symbol matches, got reasons: %v", candidates)
	}
}

func TestManifestMatch(t *testing.T) {
	tests := []struct {
		manifest string
		content  string
		target   string
		expected bool
	}{
		// Go
		{"go.mod", "module test\nrequire github.com/foo/bar v1.0.0", "github.com/foo/bar", true},
		{"go.mod", "module test\nrequire github.com/foo/bar-service v1.0.0", "github.com/foo/bar", false},
		{"go.mod", "module test\nrequire (\n\tgithub.com/foo/bar v1.0.0\n)", "github.com/foo/bar", true},

		// JS/TS
		{"package.json", `{"dependencies": {"express": "^4.17.1"}}`, "express", true},
		{"package.json", `{"dependencies": {"express-session": "^1.17.1"}}`, "express", false},

		// Python
		{"requirements.txt", "requests==2.25.1", "requests", true},
		{"requirements.txt", "requests-oauthlib==1.3.0", "requests", false},
		{"requirements.txt", "requests[security]==2.25.1", "requests", true},

		// Java
		{"pom.xml", "<dependencies><dependency><artifactId>log4j</artifactId></dependency></dependencies>", "log4j", true},
		{"pom.xml", "<dependencies><dependency><artifactId>log4j-api</artifactId></dependency></dependencies>", "log4j", false},
		{"build.gradle", "implementation 'org.slf4j:slf4j-api:1.7.30'", "slf4j-api", true},
		{"build.gradle", "implementation \"org.slf4j:slf4j-simple:1.7.30\"", "slf4j-api", false},
	}

	for _, tt := range tests {
		got := manifestMatch(tt.manifest, tt.content, tt.target)
		if got != tt.expected {
			t.Errorf("manifestMatch(%s, ..., %s) = %v; want %v", tt.manifest, tt.target, got, tt.expected)
		}
	}
}
