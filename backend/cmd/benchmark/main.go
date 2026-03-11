package main

import (
	"code-review/backend/internal/chunker"
	"code-review/backend/internal/codegraph"
	"code-review/backend/internal/llm"
	"code-review/backend/internal/models"
	"code-review/backend/internal/orchestrator"
	"code-review/backend/internal/remotefetch"
	"code-review/backend/internal/reposelect"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joho/godotenv"
)

type Truth struct {
	ID                   string    `json:"id"`
	Language             string    `json:"language"`
	Category             string    `json:"category"`
	PRContext            PRContext `json:"pr_context"`
	ExpectedDependencies []string  `json:"expected_dependencies"`
	ExpectedCrossRepo    []string  `json:"expected_cross_repo_matches"`
	BenchmarkTags        []string  `json:"benchmark_tags"`
	Bugs                 []Bug     `json:"bugs"`
	ExpectedComments     int       `json:"expected_comments"`
}

type PRContext struct {
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	Commits []string `json:"commits"`
}

type Bug struct {
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Symbol      string `json:"symbol"`
	Description string `json:"description"`
}

type Result struct {
	Approach         string
	CaseID           string
	CaseDir          string
	Tags             []string
	Passed           bool
	Recall           float64
	Precision        float64
	DependencyRecall float64
	CrossRepoRecall  float64
	Comments         int
	Error            string
}

func main() {
	var (
		approachFlag    string
		datasetRootFlag string
		tagsFlag        string
		caseFilterFlag  string
		maxCasesFlag    int
	)

	flag.StringVar(&approachFlag, "approach", "production", "benchmark approach: baseline|production|both")
	flag.StringVar(&datasetRootFlag, "dataset-root", "", "dataset root path (optional)")
	flag.StringVar(&tagsFlag, "tags", "", "comma-separated benchmark tags filter (optional)")
	flag.StringVar(&caseFilterFlag, "case-filter", "", "run only cases whose ID or path contains this value")
	flag.IntVar(&maxCasesFlag, "max-cases", 0, "max number of cases to run after filtering (0 = all)")
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		fmt.Printf("Note: .env file not found, using system environment variables\n")
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		fmt.Println("❌ Error: GEMINI_API_KEY is required")
		os.Exit(1)
	}

	approaches, err := parseApproaches(approachFlag)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	datasetRoot, err := resolveDatasetRoot(datasetRootFlag)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
	absRoot, _ := filepath.Abs(datasetRoot)

	tagFilters := parseCSV(tagsFlag)
	sort.Strings(tagFilters)

	fmt.Printf("📊 Starting Benchmark Evaluation on: %s\n", absRoot)
	fmt.Printf("⚙️  Approaches: %s\n", strings.Join(approaches, ", "))
	if len(tagFilters) > 0 {
		fmt.Printf("🏷️  Tag Filter: %s\n", strings.Join(tagFilters, ", "))
	}
	if caseFilterFlag != "" {
		fmt.Printf("🔎 Case Filter: %s\n", caseFilterFlag)
	}

	var results []Result
	var selected int

	var truthPaths []string
	filepath.Walk(absRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == "truth.json" {
			truthPaths = append(truthPaths, path)
		}
		return nil
	})
	sort.Strings(truthPaths)

	for _, truthPath := range truthPaths {
		truth, err := loadTruth(truthPath)
		if err != nil {
			results = append(results, Result{
				Approach: "n/a",
				CaseID:   filepath.Base(filepath.Dir(truthPath)),
				CaseDir:  filepath.Dir(truthPath),
				Error:    fmt.Sprintf("Failed to parse truth.json: %v", err),
			})
			continue
		}
		caseID := caseIDFromTruth(truth, truthPath)
		if caseFilterFlag != "" && !matchesCaseFilter(caseID, truthPath, caseFilterFlag) {
			continue
		}
		if len(tagFilters) > 0 && !matchesTagFilter(truth.BenchmarkTags, tagFilters) {
			continue
		}

		for _, approach := range approaches {
			res := runEvalCase(truthPath, truth, apiKey, approach)
			results = append(results, res)
		}

		selected++
		if maxCasesFlag > 0 && selected >= maxCasesFlag {
			break
		}
	}

	if selected == 0 {
		fmt.Println("❌ No benchmark cases matched the current filters.")
		os.Exit(1)
	}

	printSummary(results)
}

func runEvalCase(truthPath string, truth Truth, apiKey, approach string) Result {
	caseDir := filepath.Dir(truthPath)
	caseID := caseIDFromTruth(truth, truthPath)

	fmt.Printf("\n🔍 Evaluating Case: %s [%s]\n", caseID, approach)

	// 1. Load Diff and normalize file paths so chunking/diff parsing align with buggy file paths.
	diffPath := filepath.Join(caseDir, "diff.patch")
	diff, err := os.ReadFile(diffPath)
	if err != nil {
		return Result{
			Approach: approach,
			CaseID:   caseID,
			CaseDir:  caseDir,
			Tags:     truth.BenchmarkTags,
			Error:    "Failed to read diff.patch",
		}
	}
	normalizedDiff := normalizeDatasetDiff(string(diff))

	// 2. Materialize case as a temporary git repo so code graph scanner and tools work.
	repoPath, changedFiles, cleanup, err := materializeCaseRepo(caseDir)
	if err != nil {
		return Result{
			Approach: approach,
			CaseID:   caseID,
			CaseDir:  caseDir,
			Tags:     truth.BenchmarkTags,
			Error:    fmt.Sprintf("Failed to materialize case repo: %v", err),
		}
	}
	defer cleanup()

	// 3. Build context
	ctx := context.Background()
	providerStr := os.Getenv("LLM_PROVIDER")
	var provider llm.LLMProvider

	if providerStr == "openai" {
		provider = llm.NewOpenAIProvider(apiKey, "")
	} else if providerStr == "claude" {
		provider = llm.NewClaudeProvider(apiKey, "")
	} else {
		provider, err = llm.NewGeminiProvider(apiKey, "")
		if err != nil {
			return Result{
				Approach: approach,
				CaseID:   caseID,
				CaseDir:  caseDir,
				Tags:     truth.BenchmarkTags,
				Error:    fmt.Sprintf("Failed to init LLM provider: %v", err),
			}
		}
	}

	repoStructure := buildRepoStructure(repoPath)

	settings := models.RepoSettings{
		SecurityEnabled:     true,
		BugEnabled:          true,
		LintEnabled:         true,
		PerformanceEnabled:  true,
		ArchitectureEnabled: true,
	}

	prContext := models.PRContext{
		Title:          truth.PRContext.Title,
		Body:           truth.PRContext.Body,
		CommitMessages: truth.PRContext.Commits,
	}

	if prContext.Title == "" {
		prContext.Title = "Evaluation Test"
	}

	cgService := codegraph.NewService(repoPath)
	fullGraph, buildErr := codegraph.BuildFull(ctx, repoPath)
	if buildErr != nil {
		fmt.Printf("   ⚠️  Code graph full build failed: %v\n", buildErr)
	} else {
		cgService.SetGraph(fullGraph)
	}

	fmt.Printf("   🔭 Running Code Graph context analysis...\n")
	dependencies := make(map[string]string)
	matchSummary := ""
	crossRepoRecall := 1.0
	var remoteGraphs map[string]*codegraph.RemoteRepoGraph
	var remoteFetch *remotefetch.Fetcher
	cgContext, contextErr := cgService.GetContext(ctx, changedFiles)
	if contextErr != nil {
		fmt.Printf("   ⚠️  Code Graph context failed: %v\n", contextErr)
	} else {
		for path, content := range cgContext {
			dependencies[path] = content
		}
	}
	depRecall, depFound := dependencyRecall(truth.ExpectedDependencies, dependencies)
	if len(truth.ExpectedDependencies) > 0 {
		fmt.Printf("   🔍 Code Graph Recall: %.2f (%d/%d found)\n", depRecall, depFound, len(truth.ExpectedDependencies))
	}

	remoteBaseDir, remoteGraphs, remoteBuildErr := materializeRemoteRepos(ctx, caseDir)
	if remoteBuildErr != nil {
		return Result{
			Approach: approach,
			CaseID:   caseID,
			CaseDir:  caseDir,
			Tags:     truth.BenchmarkTags,
			Error:    fmt.Sprintf("Failed to materialize remote repos: %v", remoteBuildErr),
		}
	}
	if remoteBaseDir != "" {
		defer os.RemoveAll(remoteBaseDir)
	}

	if cgService.Graph != nil && len(remoteGraphs) > 0 {
		matches := reposelect.MatchRepos(reposelect.ExtractLocalSignals(cgService.Graph, changedFiles), remoteGraphs)
		matchSummary = reposelect.FormatMatchSummary(matches)
		crossRepoRecall = crossRepoRecallScore(truth.ExpectedCrossRepo, matches)
		remoteFetch = remotefetch.NewFetcher(remoteBaseDir, 10, 250, 500000)

		fmt.Printf("   🌐 Cross-Repo Matches: %.2f (%d expected)\n", crossRepoRecall, len(truth.ExpectedCrossRepo))
		if matchSummary != "" {
			fmt.Printf("%s\n", indentBlock(matchSummary, "      "))
		}
	} else if len(truth.ExpectedCrossRepo) > 0 {
		crossRepoRecall = 0.0
		fmt.Printf("   🌐 Cross-Repo Matches: %.2f (%d expected)\n", crossRepoRecall, len(truth.ExpectedCrossRepo))
	}

	// 4. Run review
	review, err := runReviewByApproach(
		approach,
		ctx,
		provider,
		normalizedDiff,
		changedFiles,
		dependencies,
		settings,
		repoStructure,
		prContext,
		cgService,
		repoPath,
		matchSummary,
		remoteGraphs,
		remoteFetch,
	)
	if err != nil {
		return Result{
			Approach: approach,
			CaseID:   caseID,
			CaseDir:  caseDir,
			Tags:     truth.BenchmarkTags,
			Error:    fmt.Sprintf("Review failed: %v", err),
		}
	}

	fmt.Printf("   📝 Generated Comments:\n")
	for _, c := range review.Comments {
		fmt.Printf("      - [%s] %s:%d: %s\n", c.Severity, c.File, c.Line, c.Message)
	}

	// 5. Calculate metrics
	tp := countTruePositives(truth.Bugs, review.Comments)
	recall := 1.0
	if len(truth.Bugs) > 0 {
		recall = float64(tp) / float64(len(truth.Bugs))
	}

	precision := 1.0
	if len(review.Comments) > 0 {
		precision = float64(tp) / float64(len(review.Comments))
	} else if len(truth.Bugs) > 0 {
		precision = 0.0
	}

	minDepRecall := 1.0
	if len(truth.ExpectedDependencies) > 0 {
		minDepRecall = 0.5
	}
	passed := recall >= 1.0 && depRecall >= minDepRecall && crossRepoRecall >= 1.0

	fmt.Printf("   ✅ Recall: %.2f | Precision: %.2f | Comments: %d\n", recall, precision, len(review.Comments))

	return Result{
		Approach:         approach,
		CaseID:           caseID,
		CaseDir:          caseDir,
		Tags:             truth.BenchmarkTags,
		Passed:           passed,
		Recall:           recall,
		Precision:        precision,
		DependencyRecall: depRecall,
		CrossRepoRecall:  crossRepoRecall,
		Comments:         len(review.Comments),
	}
}

func runReviewByApproach(
	approach string,
	ctx context.Context,
	provider llm.LLMProvider,
	diff string,
	changedFiles map[string]string,
	dependencies map[string]string,
	settings models.RepoSettings,
	repoStructure string,
	prContext models.PRContext,
	cgService *codegraph.Service,
	repoPath string,
	matchSummary string,
	remoteGraphs map[string]*codegraph.RemoteRepoGraph,
	remoteFetch *remotefetch.Fetcher,
) (*models.ReviewResult, error) {
	switch approach {
	case "baseline":
		return llm.RunReview(ctx, provider, diff, changedFiles, dependencies, settings, repoStructure, prContext)
	case "production":
		var graphEdges []codegraph.Edge
		if cgService.Graph != nil {
			graphEdges = cgService.Graph.Edges
		}
		chunks := chunker.GroupFiles(changedFiles, diff, graphEdges, chunker.DefaultTokenBudget)
		fmt.Printf("   📦 Chunking: %d files → %d chunk(s)\n", len(changedFiles), len(chunks))

		var results []*models.ReviewResult
		for _, chunk := range chunks {
			scopedDeps := scopeDependencies(dependencies, chunk)
			r, err := orchestrator.ReviewChunk(
				ctx,
				provider,
				chunk.Files,
				chunk.Diff,
				chunk.Index,
				chunk.Total,
				chunk.CrossRefs,
				scopedDeps,
				repoStructure,
				prContext,
				cgService.Graph,
				repoPath,
				matchSummary,
				remoteGraphs,
				remoteFetch,
				nil, // no developer rules in benchmarks
			)
			if err != nil {
				fmt.Printf("   ❌ Chunk %d/%d failed: %v\n", chunk.Index, chunk.Total, err)
				continue
			}
			results = append(results, r)
		}
		if len(results) == 0 {
			return nil, fmt.Errorf("all chunks failed")
		}
		return llm.ConsolidateResults(results), nil
	default:
		return nil, fmt.Errorf("unsupported approach: %s", approach)
	}
}

func scopeDependencies(allDeps map[string]string, chunk chunker.Chunk) map[string]string {
	if len(allDeps) == 0 {
		return allDeps
	}

	if chunk.Total == 1 {
		return allDeps
	}

	scoped := make(map[string]string)
	for depPath, content := range allDeps {
		if strings.HasPrefix(content, "CONTEXT GRAPH") || strings.HasPrefix(content, "DATA FLOW") {
			scoped[depPath] = content
			continue
		}

		depDir := filepath.Dir(depPath)
		for chunkFile := range chunk.Files {
			if filepath.Dir(chunkFile) == depDir {
				scoped[depPath] = content
				break
			}
		}

		for _, ref := range chunk.CrossRefs {
			if ref == depPath {
				scoped[depPath] = content
				break
			}
		}
	}

	if len(scoped) == 0 {
		minimal := make(map[string]string)
		for path, content := range allDeps {
			if strings.HasPrefix(content, "CONTEXT GRAPH") || strings.HasPrefix(content, "DATA FLOW") {
				minimal[path] = content
			}
		}
		return minimal
	}

	return scoped
}

func printSummary(results []Result) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("                               BENCHMARK SUMMARY")
	fmt.Println(strings.Repeat("=", 80))

	byApproach := make(map[string][]Result)
	for _, res := range results {
		byApproach[res.Approach] = append(byApproach[res.Approach], res)
	}

	approaches := make([]string, 0, len(byApproach))
	for approach := range byApproach {
		approaches = append(approaches, approach)
	}
	sort.Strings(approaches)

	for _, approach := range approaches {
		approachResults := byApproach[approach]
		sort.Slice(approachResults, func(i, j int) bool {
			return approachResults[i].CaseID < approachResults[j].CaseID
		})

		fmt.Printf("\nApproach: %s\n", approach)
		fmt.Println(strings.Repeat("-", 80))

		total := 0
		passed := 0
		avgRecall := 0.0
		avgPrecision := 0.0
		avgDepRecall := 0.0
		avgCrossRepoRecall := 0.0
		errors := 0

		for _, res := range approachResults {
			if res.Error != "" {
				errors++
				fmt.Printf("%-18s | ERROR: %s\n", res.CaseID, res.Error)
				continue
			}

			total++
			status := "❌ FAIL"
			if res.Passed {
				status = "✅ PASS"
				passed++
			}
			fmt.Printf("%-18s | Recall: %.2f | Prec: %.2f | DepRec: %.2f | XRepo: %.2f | %s\n",
				res.CaseID, res.Recall, res.Precision, res.DependencyRecall, res.CrossRepoRecall, status)

			avgRecall += res.Recall
			avgPrecision += res.Precision
			avgDepRecall += res.DependencyRecall
			avgCrossRepoRecall += res.CrossRepoRecall
		}

		if total > 0 {
			fmt.Println(strings.Repeat("-", 80))
			fmt.Printf("Total Cases: %d | Passed: %d (%.0f%%) | Errors: %d\n",
				total, passed, (float64(passed)/float64(total))*100, errors)
			fmt.Printf("Avg Recall: %.2f | Avg Precision: %.2f | Avg DepRecall: %.2f | Avg XRepo: %.2f\n",
				avgRecall/float64(total), avgPrecision/float64(total), avgDepRecall/float64(total), avgCrossRepoRecall/float64(total))
		}
	}

	if len(approaches) > 1 {
		printComparativeDelta(byApproach)
	}

	fmt.Println(strings.Repeat("=", 80))
}

func printComparativeDelta(byApproach map[string][]Result) {
	baselineResults, hasBaseline := byApproach["baseline"]
	prodResults, hasProd := byApproach["production"]
	if !hasBaseline || !hasProd {
		return
	}

	baseMap := make(map[string]Result)
	for _, r := range baselineResults {
		baseMap[r.CaseID] = r
	}
	prodMap := make(map[string]Result)
	for _, r := range prodResults {
		prodMap[r.CaseID] = r
	}

	var caseIDs []string
	for caseID := range baseMap {
		if _, ok := prodMap[caseID]; ok {
			caseIDs = append(caseIDs, caseID)
		}
	}
	sort.Strings(caseIDs)

	if len(caseIDs) == 0 {
		return
	}

	fmt.Println("\nΔ Production vs Baseline (same case)")
	fmt.Println(strings.Repeat("-", 80))
	for _, caseID := range caseIDs {
		b := baseMap[caseID]
		p := prodMap[caseID]
		if b.Error != "" || p.Error != "" {
			fmt.Printf("%-18s | skipped delta (error in one approach)\n", caseID)
			continue
		}
		fmt.Printf("%-18s | ΔRecall: %+0.2f | ΔPrec: %+0.2f | ΔDepRec: %+0.2f | ΔXRepo: %+0.2f\n",
			caseID, p.Recall-b.Recall, p.Precision-b.Precision, p.DependencyRecall-b.DependencyRecall, p.CrossRepoRecall-b.CrossRepoRecall)
	}
}

func parseApproaches(flagValue string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(flagValue)) {
	case "baseline":
		return []string{"baseline"}, nil
	case "production":
		return []string{"production"}, nil
	case "both":
		return []string{"baseline", "production"}, nil
	default:
		return nil, fmt.Errorf("invalid -approach=%q (allowed: baseline|production|both)", flagValue)
	}
}

func resolveDatasetRoot(flagValue string) (string, error) {
	if flagValue != "" {
		if isDir(flagValue) {
			return flagValue, nil
		}
		return "", fmt.Errorf("dataset root not found: %s", flagValue)
	}

	candidates := []string{
		"./datasets",
		"../datasets",
		"../../datasets",
		"../../../datasets",
	}

	for _, candidate := range candidates {
		if isDir(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not locate datasets directory (tried: %s)", strings.Join(candidates, ", "))
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func parseCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{})
	var out []string
	for _, p := range parts {
		v := strings.ToLower(strings.TrimSpace(p))
		if v == "" {
			continue
		}
		if _, exists := seen[v]; exists {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func loadTruth(truthPath string) (Truth, error) {
	data, err := os.ReadFile(truthPath)
	if err != nil {
		return Truth{}, err
	}
	var truth Truth
	if err := json.Unmarshal(data, &truth); err != nil {
		return Truth{}, err
	}
	return truth, nil
}

func caseIDFromTruth(truth Truth, truthPath string) string {
	if strings.TrimSpace(truth.ID) != "" {
		return truth.ID
	}
	return filepath.Base(filepath.Dir(truthPath))
}

func matchesCaseFilter(caseID, casePath, filter string) bool {
	f := strings.ToLower(strings.TrimSpace(filter))
	if f == "" {
		return true
	}
	return strings.Contains(strings.ToLower(caseID), f) || strings.Contains(strings.ToLower(casePath), f)
}

func matchesTagFilter(caseTags, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	if len(caseTags) == 0 {
		return false
	}

	lookup := make(map[string]struct{}, len(caseTags))
	for _, tag := range caseTags {
		lookup[strings.ToLower(strings.TrimSpace(tag))] = struct{}{}
	}
	for _, filter := range filters {
		if _, ok := lookup[filter]; ok {
			return true
		}
	}
	return false
}

func dependencyRecall(expected []string, found map[string]string) (float64, int) {
	if len(expected) == 0 {
		return 1.0, 0
	}

	foundPaths := make([]string, 0, len(found))
	for p := range found {
		foundPaths = append(foundPaths, filepath.ToSlash(p))
	}

	matched := 0
	for _, expectedPath := range expected {
		exp := filepath.ToSlash(expectedPath)
		isFound := false
		for _, actual := range foundPaths {
			if actual == exp || strings.HasSuffix(actual, exp) {
				isFound = true
				break
			}
		}
		if isFound {
			matched++
		}
	}
	return float64(matched) / float64(len(expected)), matched
}

func countTruePositives(bugs []Bug, comments []models.ReviewComment) int {
	tp := 0
	for _, bug := range bugs {
		bugFile := filepath.ToSlash(bug.File)
		found := false
		for _, comment := range comments {
			commentFile := filepath.ToSlash(comment.File)
			if commentFile == bugFile && (comment.Line >= bug.Line-5 && comment.Line <= bug.Line+5) {
				found = true
				break
			}
		}
		if found {
			tp++
		}
	}
	return tp
}

func materializeCaseRepo(caseDir string) (repoPath string, changedFiles map[string]string, cleanup func(), err error) {
	buggyDir := filepath.Join(caseDir, "buggy")
	info, statErr := os.Stat(buggyDir)
	if statErr != nil || !info.IsDir() {
		return "", nil, nil, fmt.Errorf("missing buggy directory: %s", buggyDir)
	}

	tmpDir, err := os.MkdirTemp("", "ai-review-benchmark-*")
	if err != nil {
		return "", nil, nil, err
	}
	cleanup = func() { _ = os.RemoveAll(tmpDir) }

	if err := copyDirContents(buggyDir, tmpDir); err != nil {
		cleanup()
		return "", nil, nil, fmt.Errorf("copy buggy files: %w", err)
	}

	depsDir := filepath.Join(caseDir, "deps")
	if depsInfo, depErr := os.Stat(depsDir); depErr == nil && depsInfo.IsDir() {
		if err := copyDirContents(depsDir, tmpDir); err != nil {
			cleanup()
			return "", nil, nil, fmt.Errorf("copy deps files: %w", err)
		}
	}

	changedFiles = make(map[string]string)
	err = filepath.Walk(buggyDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(buggyDir, path)
		if relErr != nil {
			return relErr
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		changedFiles[filepath.ToSlash(rel)] = string(content)
		return nil
	})
	if err != nil {
		cleanup()
		return "", nil, nil, fmt.Errorf("load changed files: %w", err)
	}

	if err := initGitRepo(tmpDir); err != nil {
		fmt.Printf("   ⚠️  Failed to initialize temporary git repo: %v\n", err)
	}

	return tmpDir, changedFiles, cleanup, nil
}

func materializeRemoteRepos(ctx context.Context, caseDir string) (string, map[string]*codegraph.RemoteRepoGraph, error) {
	remoteRoot := filepath.Join(caseDir, "remote_repos")
	if !isDir(remoteRoot) {
		return "", nil, nil
	}

	tmpDir, err := os.MkdirTemp("", "ai-review-benchmark-remote-*")
	if err != nil {
		return "", nil, err
	}

	graphs := make(map[string]*codegraph.RemoteRepoGraph)

	owners, err := os.ReadDir(remoteRoot)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", nil, err
	}

	for _, ownerEntry := range owners {
		if !ownerEntry.IsDir() {
			continue
		}

		ownerDir := filepath.Join(remoteRoot, ownerEntry.Name())
		repos, readErr := os.ReadDir(ownerDir)
		if readErr != nil {
			_ = os.RemoveAll(tmpDir)
			return "", nil, readErr
		}

		for _, repoEntry := range repos {
			if !repoEntry.IsDir() {
				continue
			}

			repoFullName := ownerEntry.Name() + "/" + repoEntry.Name()
			srcDir := filepath.Join(ownerDir, repoEntry.Name())
			dstDir := filepath.Join(tmpDir, ownerEntry.Name(), repoEntry.Name())
			if err := copyDirContents(srcDir, dstDir); err != nil {
				_ = os.RemoveAll(tmpDir)
				return "", nil, fmt.Errorf("copy remote repo %s: %w", repoFullName, err)
			}

			graph, err := codegraph.BuildRemoteGraph(ctx, repoFullName, dstDir)
			if err != nil {
				_ = os.RemoveAll(tmpDir)
				return "", nil, fmt.Errorf("build remote graph %s: %w", repoFullName, err)
			}
			graphs[repoFullName] = graph
		}
	}

	return tmpDir, graphs, nil
}

func crossRepoRecallScore(expected []string, matches []reposelect.CandidateMatch) float64 {
	if len(expected) == 0 {
		if len(matches) == 0 {
			return 1.0
		}
		return 0.0
	}

	found := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		found[strings.ToLower(match.RepoFullName)] = struct{}{}
	}

	matched := 0
	for _, repo := range expected {
		if _, ok := found[strings.ToLower(repo)]; ok {
			matched++
		}
	}

	return float64(matched) / float64(len(expected))
}

func indentBlock(input, prefix string) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}

	lines := strings.Split(input, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func copyDirContents(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	from, err := os.Open(src)
	if err != nil {
		return err
	}
	defer from.Close()

	to, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer to.Close()

	_, err = io.Copy(to, from)
	return err
}

func initGitRepo(repoPath string) error {
	commands := [][]string{
		{"git", "init", "-q"},
		{"git", "add", "-A"},
		{"git", "-c", "user.name=benchmark", "-c", "user.email=benchmark@example.com", "commit", "-qm", "benchmark case"},
	}
	for _, cmdArgs := range commands {
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			// Allow "nothing to commit" without failing setup.
			if strings.Contains(string(out), "nothing to commit") {
				return nil
			}
			return fmt.Errorf("%s failed: %v (%s)", strings.Join(cmdArgs, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func buildRepoStructure(repoPath string) string {
	const maxEntries = 4000

	var entries []string
	_ = filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(repoPath, path)
		if relErr != nil {
			return nil
		}
		entries = append(entries, filepath.ToSlash(rel))
		if len(entries) >= maxEntries {
			return fmt.Errorf("stop")
		}
		return nil
	})
	sort.Strings(entries)
	return strings.Join(entries, "\n")
}

func normalizeDatasetDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				aPath := normalizePatchPath(strings.TrimPrefix(parts[2], "a/"))
				bPath := normalizePatchPath(strings.TrimPrefix(parts[3], "b/"))
				lines[i] = fmt.Sprintf("diff --git a/%s b/%s", aPath, bPath)
			}
		case strings.HasPrefix(line, "--- "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "--- "))
			if path != "/dev/null" {
				lines[i] = fmt.Sprintf("--- a/%s", normalizePatchPath(strings.TrimPrefix(path, "a/")))
			}
		case strings.HasPrefix(line, "+++ "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			if path != "/dev/null" {
				lines[i] = fmt.Sprintf("+++ b/%s", normalizePatchPath(strings.TrimPrefix(path, "b/")))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func normalizePatchPath(path string) string {
	p := strings.Trim(path, "\"")
	p = filepath.ToSlash(p)
	if idx := strings.Index(p, "/buggy/"); idx >= 0 {
		return strings.TrimPrefix(p[idx+len("/buggy/"):], "/")
	}
	if idx := strings.Index(p, "/fixed/"); idx >= 0 {
		return strings.TrimPrefix(p[idx+len("/fixed/"):], "/")
	}
	if strings.HasPrefix(p, "buggy/") {
		return strings.TrimPrefix(p, "buggy/")
	}
	if strings.HasPrefix(p, "fixed/") {
		return strings.TrimPrefix(p, "fixed/")
	}
	return p
}
