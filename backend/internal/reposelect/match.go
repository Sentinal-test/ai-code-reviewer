package reposelect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"code-review/backend/internal/codegraph"
	"code-review/backend/internal/multirepo"

	"github.com/google/go-github/v60/github"
)

// LocalSignals contains facts extracted from the PR's local graph.
// These represent the "surface area" of the PR changes that might affect other repos.
type LocalSignals struct {
	ProjectName    string            // e.g., module name from go.mod or name from package.json
	ChangedExports map[string]string // map[SymbolName]Language
	ChangedImports map[string]string // map[ImportPath]Language
	// Placeholders for advanced extraction:
	// ChangedHTTPRoutes map[string]bool
	// ChangedTopics     map[string]bool
	// ChangedGRPC       map[string]bool
}

// RemoteFacts contains the targetable surface area of a remote repository.
type RemoteFacts struct {
	Imports map[string]string // map[ImportPath]Language
	Exports map[string]string // map[SymbolName]Language
}

// ExtractLocalSignals analyzes the local code graph + the diff (changedFiles)
// to identify what changed that a remote repo might care about.
func ExtractLocalSignals(localGraph *codegraph.Graph, changedFiles map[string]string) LocalSignals {
	signals := LocalSignals{
		ChangedExports: make(map[string]string),
		ChangedImports: make(map[string]string),
	}

	for path := range changedFiles {
		if localGraph == nil {
			continue
		}
		if entry, ok := localGraph.Files[path]; ok {
			// 1. Extract changed exports (Definitions)
			for _, def := range entry.Definitions {
				if IsExported(entry.Language, def.Symbol) {
					signals.ChangedExports[def.Symbol] = entry.Language
				}
			}

			// 2. Extract changed imports
			for _, imp := range entry.Imports {
				signals.ChangedImports[imp] = entry.Language
			}
		}
	}

	return signals
}

// ExtractRemoteFacts flattens a remote graph into matchable lists.
func ExtractRemoteFacts(remoteGraph *codegraph.RemoteRepoGraph) RemoteFacts {
	facts := RemoteFacts{
		Imports: make(map[string]string),
		Exports: make(map[string]string),
	}

	for _, entry := range remoteGraph.Files {
		for _, imp := range entry.Imports {
			facts.Imports[imp] = entry.Language
		}
		for _, def := range entry.Definitions {
			if IsExported(entry.Language, def.Symbol) {
				facts.Exports[def.Symbol] = entry.Language
			}
		}
	}

	return facts
}

// CandidateMatch represents a single deterministic match between the PR and a remote repo.
type CandidateMatch struct {
	RepoFullName string
	Priority     int    // 1 (highest) to 6 (lowest)
	Reason       string // e.g., "Direct module import match"
	Evidence     string // e.g., "imports 'code-review/backend/internal/models'"
}

// MatchRepos runs the deterministic rules against remote facts using the local signals.
// IdentifyRelevantRepos uses the GitHub API to fetch manifests and identify which repos are likely relevant.
// This allows us to skip cloning 100s of irrelevant repositories.
func IdentifyRelevantRepos(ctx context.Context, client *github.Client, signals LocalSignals, repos []multirepo.DiscoveredRepo) []multirepo.DiscoveredRepo {
	var relevant []multirepo.DiscoveredRepo

	// Manifests to check per language
	manifests := []string{
		"go.mod",                                         // Go
		"package.json",                                   // JS/TS
		"requirements.txt", "pyproject.toml", "setup.py", // Python
		"pom.xml", "build.gradle", // Java
	}

	fmt.Printf("   🔍 [LightweightFilter] Checking %d repos for dependency match to '%s'...\n", len(repos), signals.ProjectName)

	for _, repo := range repos {
		matched := false
		for _, manifest := range manifests {
			fileContent, _, _, err := client.Repositories.GetContents(ctx, repo.Owner, repo.Name, manifest, nil)
			if err != nil || fileContent == nil {
				continue
			}

			content, err := fileContent.GetContent()
			if err != nil {
				continue
			}

			if signals.ProjectName != "" && manifestMatch(manifest, content, signals.ProjectName) {
				fmt.Printf("      ✅ %s matched via %s (depends on %s)\n", repo.FullName, manifest, signals.ProjectName)
				matched = true
				break
			}

			for imp, lang := range signals.ChangedImports {
				// Avoid matching on very short or standard library imports (e.g., "io", "fmt", "os")
				if len(imp) < 5 || IsStandardLibrary(lang, imp) {
					continue
				}
				if manifestMatch(manifest, content, imp) {
					fmt.Printf("      ✅ %s matched via %s (shares import: %s)\n", repo.FullName, manifest, imp)
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}

		if matched {
			relevant = append(relevant, repo)
		}
	}

	return relevant
}

// manifestMatch performs a language-aware check to see if a dependency is present in a manifest file.
// It avoids simple substring matches to prevent false positives like "log" matching "github.com/sirupsen/logrus".
func manifestMatch(manifest, content, target string) bool {
	if target == "" {
		return false
	}

	switch filepath.Base(manifest) {
	case "go.mod":
		// Go dependencies are usually on their own line with leading whitespace, 
		// or in a require block: "github.com/foo/bar v1.2.3" or "require github.com/foo/bar"
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module ") {
				continue
			}
			// require github.com/foo/bar v1.2.3
			if strings.Contains(line, target) {
				parts := strings.Fields(line)
				for _, p := range parts {
					if p == target {
						return true
					}
				}
			}
		}

	case "package.json":
		// "dependencies": { "target": "version" }
		// Simple quoted match for the key
		quoted := fmt.Sprintf("\"%s\"", target)
		return strings.Contains(content, quoted)

	case "requirements.txt":
		// target==version or target>=version
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// requirements can have [extras], split by [ or == or >=
			pkg := strings.FieldsFunc(line, func(r rune) bool {
				return r == '=' || r == '>' || r == '<' || r == '[' || r == '~' || r == '!'
			})[0]
			if strings.TrimSpace(pkg) == target {
				return true
			}
		}

	case "pom.xml":
		// Simple check for target inside <artifactId> or <groupId>
		tag1 := fmt.Sprintf("<artifactId>%s</artifactId>", target)
		tag2 := fmt.Sprintf("<groupId>%s</groupId>", target)
		return strings.Contains(content, tag1) || strings.Contains(content, tag2)

	case "build.gradle":
		// implementation 'group:artifact:version' or "group:artifact:version"
		quoted1 := fmt.Sprintf("'%s'", target)
		quoted2 := fmt.Sprintf("\"%s\"", target)
		if strings.Contains(content, quoted1) || strings.Contains(content, quoted2) {
			return true
		}
		// Also check colon-separated format
		return strings.Contains(content, ":"+target+":") || strings.Contains(content, ":"+target+"'") || strings.Contains(content, ":"+target+"\"")

	default:
		// Fallback for others (pyproject.toml, etc.)
		quoted1 := fmt.Sprintf("'%s'", target)
		quoted2 := fmt.Sprintf("\"%s\"", target)
		return strings.Contains(content, quoted1) || strings.Contains(content, quoted2)
	}

	return false
}

// ExtractProjectName identifies the name/module-id of the current repository.
func ExtractProjectName(repoPath string) string {
	// 1. Go (go.mod)
	if data, err := os.ReadFile(filepath.Join(repoPath, "go.mod")); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "module ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "module "))
			}
		}
	}

	// 2. JS/TS (package.json)
	if data, err := os.ReadFile(filepath.Join(repoPath, "package.json")); err == nil {
		var pkg struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &pkg); err == nil && pkg.Name != "" {
			return pkg.Name
		}
	}

	// Fallback to directory name
	return filepath.Base(repoPath)
}

func MatchRepos(signals LocalSignals, remoteGraphs map[string]*codegraph.RemoteRepoGraph) []CandidateMatch {
	var candidates []CandidateMatch

	for repoName, rGraph := range remoteGraphs {
		facts := ExtractRemoteFacts(rGraph)

		matched := false

		// Rule 1: Direct module import match
		// Check if the remote repo imports something the local repo changed.
		// (In a real monorepo/polyrepo setup, you'd match the module prefix).
		for imp, lang := range facts.Imports {
			// Skip standard library imports based on language
			if IsStandardLibrary(lang, imp) {
				continue
			}

			if signals.ChangedImports[imp] != "" {
				candidates = append(candidates, CandidateMatch{
					RepoFullName: repoName,
					Priority:     1,
					Reason:       "Shared import context match",
					Evidence:     "Both use module: " + imp,
				})
				matched = true
				break
			}
		}
		if matched {
			continue // Already high priority, move to next repo
		}

		// Rule 5: Shared symbol match
		// Check if the remote repo provides or consumes a symbol the PR changed.
		for exp, _ := range facts.Exports {
			if signals.ChangedExports[exp] != "" {
				candidates = append(candidates, CandidateMatch{
					RepoFullName: repoName,
					Priority:     5,
					Reason:       "Shared symbol match",
					Evidence:     "Symbol: " + exp,
				})
				matched = true
				break // Found a match, move to next repo
			}
		}
	}

	return candidates
}

// FormatMatchSummary produces the LLM-friendly deterministics text block.
func FormatMatchSummary(candidates []CandidateMatch) string {
	if len(candidates) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("CROSS-REPO MATCHES\n")

	for _, cand := range candidates {
		sb.WriteString("- " + cand.RepoFullName + "\n")
		sb.WriteString("  reason: " + cand.Reason + "\n")
		sb.WriteString("  evidence: " + cand.Evidence + "\n\n")
	}

	return strings.TrimSpace(sb.String())
}
