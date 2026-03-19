package reposelect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"code-review/backend/internal/codegraph"
	"code-review/backend/internal/multirepo"

	"github.com/google/go-github/v60/github"
)

// LocalSignals contains facts extracted from the PR's local graph.
// These represent the "surface area" of the PR changes that might affect other repos.
type LocalSignals struct {
	ProjectName             string            // e.g., module name from go.mod or name from package.json
	ProjectTargets          []string          // canonical dependency/import targets and aliases for this project
	ChangedExports          map[string]string // map[SymbolName]Language
	ChangedQualifiedExports map[string]string // map[QualifiedSymbol]Language
	ChangedImports          map[string]string // map[ImportPath]Language
	ChangedPackages         map[string]string // map[PackageName]Language
	ChangedPackageDirs      map[string]string // map[RelativeDir]Language
}

// RemoteFacts contains the targetable surface area of a remote repository.
type RemoteFacts struct {
	Imports          map[string]string // map[ImportPath]Language
	Exports          map[string]string // map[SymbolName]Language
	QualifiedExports map[string]string // map[QualifiedSymbol]Language
	Packages         map[string]string // map[PackageName]Language
}

// ExtractLocalSignals analyzes the local code graph + the diff (changedFiles)
// to identify what changed that a remote repo might care about.
func ExtractLocalSignals(localGraph *codegraph.Graph, changedFiles map[string]string) LocalSignals {
	signals := LocalSignals{
		ChangedExports:          make(map[string]string),
		ChangedQualifiedExports: make(map[string]string),
		ChangedImports:          make(map[string]string),
		ChangedPackages:         make(map[string]string),
		ChangedPackageDirs:      make(map[string]string),
	}

	for path := range changedFiles {
		if localGraph == nil {
			continue
		}
		if entry, ok := localGraph.Files[path]; ok {
			if entry.Package != "" {
				signals.ChangedPackages[entry.Package] = entry.Language
			}
			dir := filepath.ToSlash(filepath.Dir(path))
			if dir == "." {
				dir = ""
			}
			signals.ChangedPackageDirs[dir] = entry.Language

			for _, def := range entry.Definitions {
				if IsExported(entry.Language, def.Symbol) {
					signals.ChangedExports[def.Symbol] = entry.Language
					if def.QualifiedSymbol != "" {
						signals.ChangedQualifiedExports[def.QualifiedSymbol] = entry.Language
					}
				}
			}

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
		Imports:          make(map[string]string),
		Exports:          make(map[string]string),
		QualifiedExports: make(map[string]string),
		Packages:         make(map[string]string),
	}

	for _, entry := range remoteGraph.Files {
		if entry.Package != "" {
			facts.Packages[entry.Package] = entry.Language
		}
		for _, imp := range entry.Imports {
			facts.Imports[imp] = entry.Language
		}
		for _, def := range entry.Definitions {
			if IsExported(entry.Language, def.Symbol) {
				facts.Exports[def.Symbol] = entry.Language
				if def.QualifiedSymbol != "" {
					facts.QualifiedExports[def.QualifiedSymbol] = entry.Language
				}
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
	if len(repos) == 0 {
		return nil
	}

	manifests := []string{
		"go.mod", "package.json", "requirements.txt", "pyproject.toml",
		"setup.py", "pom.xml", "build.gradle",
	}

	fmt.Printf("   🔍 [LightweightFilter] Checking %d repos concurrently...\n", len(repos))
	rootTargets := projectRootTargets(signals)
	projectTargets := projectDependencyTargets(signals)

	type result struct {
		repo multirepo.DiscoveredRepo
		rel  bool
	}

	repoChan := make(chan multirepo.DiscoveredRepo, len(repos))
	resChan := make(chan result, len(repos))

	// Start workers (max 10 concurrent)
	numWorkers := 10
	if len(repos) < numWorkers {
		numWorkers = len(repos)
	}

	for w := 0; w < numWorkers; w++ {
		go func() {
			for repo := range repoChan {
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

					// Check project and root targets
					for _, target := range rootTargets {
						if target != "" && manifestMatch(manifest, content, target) {
							matched = true
							break
						}
					}
					if !matched {
						for _, target := range projectTargets {
							if target != "" && manifestMatch(manifest, content, target) {
								matched = true
								break
							}
						}
					}
					if !matched {
						for imp, lang := range signals.ChangedImports {
							if imp == "" || IsStandardLibrary(lang, imp) {
								continue
							}
							if manifestMatch(manifest, content, imp) {
								matched = true
								break
							}
						}
					}
					if matched {
						break
					}
				}
				resChan <- result{repo, matched}
			}
		}()
	}

	for _, repo := range repos {
		repoChan <- repo
	}
	close(repoChan)

	var relevant []multirepo.DiscoveredRepo
	for i := 0; i < len(repos); i++ {
		res := <-resChan
		if res.rel {
			relevant = append(relevant, res.repo)
		}
	}

	// Sort result for determinism
	sort.Slice(relevant, func(i, j int) bool {
		return relevant[i].FullName < relevant[j].FullName
	})

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
			if strings.HasPrefix(line, "module") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "module" {
					continue
				}
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

// ExtractProjectName identifies the primary name/module-id of the current repository.
func ExtractProjectName(repoPath string) string {
	targets := ExtractProjectTargets(repoPath)
	if len(targets) > 0 {
		return targets[0]
	}
	return filepath.Base(repoPath)
}

// ExtractProjectTargets identifies canonical dependency/import targets and practical aliases
// for the current repository. This makes matching resilient when repo names and module paths drift.
func ExtractProjectTargets(repoPath string) []string {
	seen := make(map[string]struct{})
	var targets []string

	add := func(value string) {
		value = strings.TrimSpace(strings.TrimSuffix(value, ".git"))
		value = strings.Trim(value, "/")
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		targets = append(targets, value)
	}

	// 1. Go module path
	if data, err := os.ReadFile(filepath.Join(repoPath, "go.mod")); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "module" {
					add(fields[1])
					break
				}
			}
		}
	}

	// 2. JS/TS package name
	if data, err := os.ReadFile(filepath.Join(repoPath, "package.json")); err == nil {
		var pkg struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &pkg); err == nil && pkg.Name != "" {
			add(pkg.Name)
		}
	}

	// 3. Origin remote aliases
	if hostPath, ownerRepo := extractGitRemoteTargets(repoPath); hostPath != "" || ownerRepo != "" {
		add(hostPath)
		add(ownerRepo)
		if ownerRepo != "" {
			if parts := strings.Split(ownerRepo, "/"); len(parts) > 0 {
				add(parts[len(parts)-1])
			}
		}
	}

	// 4. Fallback to the local directory name
	add(filepath.Base(repoPath))
	return targets
}

func MatchRepos(signals LocalSignals, remoteGraphs map[string]*codegraph.RemoteRepoGraph) []CandidateMatch {
	var candidates []CandidateMatch
	projectTargets := targetSet(projectDependencyTargets(signals))
	changedProjectTargets := changedProjectImportTargets(signals)

	for repoName, rGraph := range remoteGraphs {
		if rGraph == nil {
			continue
		}

		if evidence := remoteConsumesChangedExports(rGraph, changedProjectTargets, signals); len(evidence) > 0 {
			candidates = append(candidates, CandidateMatch{
				RepoFullName: repoName,
				Priority:     1,
				Reason:       "Consumes changed exported symbol",
				Evidence:     displayEvidence(evidence, 3),
			})
			continue
		}

		if imports := remoteProjectImports(rGraph, changedProjectTargets); len(imports) > 0 {
			candidates = append(candidates, CandidateMatch{
				RepoFullName: repoName,
				Priority:     2,
				Reason:       "Imports changed project package",
				Evidence:     displayEvidence(imports, 3),
			})
			continue
		}

		if imports := remoteProjectImports(rGraph, projectTargets); len(imports) > 0 {
			candidates = append(candidates, CandidateMatch{
				RepoFullName: repoName,
				Priority:     3,
				Reason:       "Imports current project",
				Evidence:     displayEvidence(imports, 3),
			})
			continue
		}

		facts := ExtractRemoteFacts(rGraph)
		if symbols := sharedExportedSymbols(signals, facts); len(symbols) > 0 {
			candidates = append(candidates, CandidateMatch{
				RepoFullName: repoName,
				Priority:     4,
				Reason:       "Shared exported symbol match",
				Evidence:     displayEvidence(symbols, 3),
			})
			continue
		}

		if overlap := sharedDependencyOverlap(signals, facts); len(overlap) > 0 {
			candidates = append(candidates, CandidateMatch{
				RepoFullName: repoName,
				Priority:     5,
				Reason:       "Shared dependency context",
				Evidence:     displayEvidence(overlap, 3),
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority == candidates[j].Priority {
			return candidates[i].RepoFullName < candidates[j].RepoFullName
		}
		return candidates[i].Priority < candidates[j].Priority
	})
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
		sb.WriteString(fmt.Sprintf("  priority: %d\n", cand.Priority))
		sb.WriteString("  reason: " + cand.Reason + "\n")
		sb.WriteString("  evidence: " + cand.Evidence + "\n\n")
	}

	return strings.TrimSpace(sb.String())
}

func projectDependencyTargets(signals LocalSignals) []string {
	targets := make(map[string]struct{})
	for _, target := range projectRootTargets(signals) {
		targets[target] = struct{}{}
	}

	for dir := range signals.ChangedPackageDirs {
		dir = strings.Trim(strings.TrimSpace(dir), "/")
		for _, root := range projectRootTargets(signals) {
			if root == "" {
				continue
			}
			if dir == "" {
				targets[root] = struct{}{}
				continue
			}
			targets[strings.TrimRight(root, "/")+"/"+dir] = struct{}{}
		}
	}

	out := make([]string, 0, len(targets))
	for target := range targets {
		out = append(out, target)
	}
	sort.Strings(out)
	return out
}

func changedProjectImportTargets(signals LocalSignals) map[string]string {
	targets := make(map[string]string)
	for _, target := range projectDependencyTargets(signals) {
		if target == "" {
			continue
		}
		lang := ""
		if containsString(projectRootTargets(signals), target) {
			for _, candidate := range signals.ChangedPackageDirs {
				lang = candidate
				break
			}
		}
		for dir, candidate := range signals.ChangedPackageDirs {
			expected := strings.Trim(strings.TrimSpace(dir), "/")
			for _, root := range projectRootTargets(signals) {
				if expected == "" && target == root {
					lang = candidate
				}
				if expected != "" && target == strings.TrimRight(root, "/")+"/"+expected {
					lang = candidate
				}
			}
		}
		targets[target] = lang
	}
	return targets
}

func projectRootTargets(signals LocalSignals) []string {
	if len(signals.ProjectTargets) > 0 {
		return dedupePreserveOrder(signals.ProjectTargets)
	}
	if strings.TrimSpace(signals.ProjectName) == "" {
		return nil
	}
	return []string{signals.ProjectName}
}

func targetSet(values []string) map[string]string {
	out := make(map[string]string, len(values))
	for _, value := range values {
		out[value] = ""
	}
	return out
}

func importMatchesTarget(importPath, target string) bool {
	importPath = strings.TrimSpace(importPath)
	target = strings.TrimSpace(target)
	if importPath == "" || target == "" {
		return false
	}
	if importPath == target {
		return true
	}
	return strings.HasPrefix(importPath, target+"/") || strings.HasPrefix(importPath, target+".")
}

func remoteProjectImports(graph *codegraph.RemoteRepoGraph, targets map[string]string) []string {
	if graph == nil || len(targets) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	var evidence []string
	for path, entry := range graph.Files {
		for _, imp := range entry.Imports {
			for target := range targets {
				if importMatchesTarget(imp, target) {
					line := fmt.Sprintf("%s imports %s", path, imp)
					if _, ok := seen[line]; !ok {
						seen[line] = struct{}{}
						evidence = append(evidence, line)
					}
				}
			}
		}
	}
	sort.Strings(evidence)
	return evidence
}

func remoteConsumesChangedExports(graph *codegraph.RemoteRepoGraph, targets map[string]string, signals LocalSignals) []string {
	if graph == nil || len(targets) == 0 || len(signals.ChangedExports) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	var evidence []string
	for path, entry := range graph.Files {
		for _, ref := range entry.References {
			importPath := externalImportForReference(entry, ref, targets)
			if importPath == "" {
				continue
			}
			if signals.ChangedExports[ref.Symbol] == "" {
				continue
			}
			line := fmt.Sprintf("%s:%d uses %s from %s", path, ref.Line, ref.Symbol, importPath)
			if _, ok := seen[line]; !ok {
				seen[line] = struct{}{}
				evidence = append(evidence, line)
			}
		}
	}
	sort.Strings(evidence)
	return evidence
}

func externalImportForReference(entry codegraph.RemoteFileEntry, ref codegraph.Reference, targets map[string]string) string {
	if ref.Qualifier != "" {
		if importPath, ok := entry.ImportAliases[ref.Qualifier]; ok && matchesTargetSet(importPath, targets) {
			return importPath
		}
	}
	if importPath, ok := entry.ImportAliases[ref.Symbol]; ok && matchesTargetSet(importPath, targets) {
		return importPath
	}
	return ""
}

func matchesTargetSet(importPath string, targets map[string]string) bool {
	for target := range targets {
		if importMatchesTarget(importPath, target) {
			return true
		}
	}
	return false
}

func sharedDependencyOverlap(signals LocalSignals, facts RemoteFacts) []string {
	seen := make(map[string]struct{})
	var overlap []string
	for imp, lang := range signals.ChangedImports {
		if len(imp) < 5 || IsStandardLibrary(lang, imp) {
			continue
		}
		if facts.Imports[imp] == "" {
			continue
		}
		line := "both depend on " + imp
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		overlap = append(overlap, line)
	}
	sort.Strings(overlap)
	return overlap
}

func sharedExportedSymbols(signals LocalSignals, facts RemoteFacts) []string {
	seen := make(map[string]struct{})
	var overlap []string
	for symbol := range signals.ChangedExports {
		if facts.Exports[symbol] == "" {
			continue
		}
		line := "shared export " + symbol
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		overlap = append(overlap, line)
	}
	sort.Strings(overlap)
	return overlap
}

func displayEvidence(values []string, limit int) string {
	if len(values) == 0 {
		return "none"
	}
	if len(values) <= limit {
		return strings.Join(values, "; ")
	}
	return strings.Join(values[:limit], "; ") + fmt.Sprintf(" (+%d more)", len(values)-limit)
}

func extractGitRemoteTargets(repoPath string) (hostPath string, ownerRepo string) {
	data, err := os.ReadFile(filepath.Join(repoPath, ".git", "config"))
	if err != nil {
		return "", ""
	}

	inOrigin := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") {
			// Reset flag for any new section header
			inOrigin = strings.HasPrefix(line, `[remote "origin"]`)
			continue
		}

		if inOrigin && strings.HasPrefix(line, "url") {
			// Flexible check for "url = value" or "url=value"
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == "url" {
				return normalizeRemoteURL(strings.TrimSpace(parts[1]))
			}
		}
	}
	return "", ""
}

func normalizeRemoteURL(raw string) (hostPath string, ownerRepo string) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	raw = strings.TrimSuffix(raw, ".git")
	if raw == "" {
		return "", ""
	}

	if strings.Contains(raw, "://") {
		parts := strings.SplitN(raw, "://", 2)
		remainder := parts[1]
		remainder = strings.TrimPrefix(remainder, "git@")
		remainder = strings.Trim(remainder, "/")
		if remainder == "" {
			return "", ""
		}
		return remainder, trimHostPrefix(remainder)
	}

	if idx := strings.Index(raw, ":"); idx != -1 {
		host := strings.TrimPrefix(raw[:idx], "git@")
		path := strings.Trim(raw[idx+1:], "/")
		if host != "" && path != "" {
			hostPath = host + "/" + path
			return hostPath, trimHostPrefix(hostPath)
		}
	}

	return raw, trimHostPrefix(raw)
}

func trimHostPrefix(value string) string {
	if idx := strings.Index(value, "/"); idx != -1 {
		rest := value[idx+1:]
		if strings.Count(rest, "/") >= 1 {
			return rest
		}
	}
	return value
}

func dedupePreserveOrder(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
