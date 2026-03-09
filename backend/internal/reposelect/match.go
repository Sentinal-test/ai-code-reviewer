package reposelect

import (
	"strings"

	"code-review/backend/internal/codegraph"
)

// LocalSignals contains facts extracted from the PR's local graph.
// These represent the "surface area" of the PR changes that might affect other repos.
type LocalSignals struct {
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
