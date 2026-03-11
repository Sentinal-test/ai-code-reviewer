package main

import (
	"code-review/backend/internal/action"
	"code-review/backend/internal/chunker"
	"code-review/backend/internal/codegraph"
	"code-review/backend/internal/llm"
	"code-review/backend/internal/models"
	"code-review/backend/internal/multirepo"
	"code-review/backend/internal/orchestrator"
	"code-review/backend/internal/remotefetch"
	"code-review/backend/internal/reposelect"
	"code-review/backend/internal/rules"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// checkMultiRepoAvailability returns true if the current token has access to more than 1 repository.
func checkMultiRepoAvailability(ctx context.Context, ghClient *action.GitHubClient) bool {
	if ghClient == nil {
		fmt.Println("ℹ️ Multi-repo context skipped: no GitHub client available.")
		return false
	}
	repos, err := ghClient.ListAccessibleRepos(ctx)
	if err != nil {
		fmt.Printf("ℹ️ Multi-repo context skipped: unable to list accessible repos (likely using standard token instead of App token).\n")
		return false
	}
	if len(repos) <= 1 {
		fmt.Printf("ℹ️ Multi-repo context skipped: token has access to only 1 repository.\n")
		return false
	}
	fmt.Printf("✅ Multi-repo context available! Found %d accessible repositories.\n", len(repos))
	return true
}

func main() {
	// 1. Parse Args & Env
	apiKeyFlag := flag.String("api-key", "", "API Key (default Gemini, fallback for others)")
	llmProviderFlag := flag.String("llm-provider", "gemini", "LLM Provider (gemini, openai, claude)")
	githubTokenFlag := flag.String("github-token", "", "GitHub Token")
	prNumberFlag := flag.String("pr-number", "", "Pull Request Number")
	repoNameFlag := flag.String("repo", "", "Repository Name (owner/repo)")
	commitShaFlag := flag.String("sha", "", "Commit SHA")
	appIdFlag := flag.String("app-id", "", "GitHub App ID")
	appPrivateKeyFlag := flag.String("app-private-key", "", "GitHub App Private Key")

	// Flags for local testing or overrides
	baseRef := flag.String("base", "main", "Base ref to diff against")
	headRef := flag.String("head", "HEAD", "Head ref to diff")
	dryRun := flag.Bool("dry-run", false, "Print results to stdout instead of commenting")
	noCache := flag.Bool("no-cache", false, "Skip graph cache (force fresh analysis)")
	reviewRulesFlag := flag.String("review-rules", "", "YAML block with custom review rules")
	flag.Parse()

	// 2. Resolve parameters (Priority: Flag -> Env)
	apiKey := *apiKeyFlag
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY") // legacy, acts as default API key
	}

	llmProvider := *llmProviderFlag
	if os.Getenv("LLM_PROVIDER") != "" {
		llmProvider = os.Getenv("LLM_PROVIDER")
	}

	githubToken := *githubTokenFlag
	if githubToken == "" {
		githubToken = os.Getenv("GITHUB_TOKEN")
	}

	prNumber := *prNumberFlag
	if prNumber == "" {
		prNumber = os.Getenv("PR_NUMBER")
	}

	repoName := *repoNameFlag
	if repoName == "" {
		repoName = os.Getenv("GITHUB_REPOSITORY")
	}

	commitSHA := *commitShaFlag
	if commitSHA == "" {
		commitSHA = os.Getenv("GITHUB_SHA")
	}

	appID := *appIdFlag
	if appID == "" {
		appID = os.Getenv("APP_ID")
	}

	appPrivateKey := *appPrivateKeyFlag
	if appPrivateKey == "" {
		appPrivateKey = os.Getenv("APP_PRIVATE_KEY")
	}

	if apiKey == "" && os.Getenv("OPENAI_API_KEY") == "" && os.Getenv("ANTHROPIC_API_KEY") == "" {
		fmt.Println("❌ Error: Valid API key is required. Set GEMINI_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY.")
		os.Exit(1)
	}

	// 2. Local Git Operations (Security-First: Code stays here)
	fmt.Printf("📂 Fetching diff between %s and %s...\n", *baseRef, *headRef)
	diff, err := action.GetDiff(*baseRef, *headRef)
	if err != nil {
		fmt.Printf("❌ Error getting diff: %v\n", err)
		os.Exit(1)
	}

	if len(diff) == 0 {
		fmt.Println("✅ No changes detected.")
		return
	}

	changedFilesList, err := action.GetChangedFiles(*baseRef, *headRef)
	if err != nil {
		fmt.Printf("❌ Error getting changed files: %v\n", err)
		os.Exit(1)
	}

	// Build maps for llm.RunReview
	changedFiles := make(map[string]string)
	for _, file := range changedFilesList {
		// Only read content for files we have access to
		content, err := action.GetFileContent(file)
		if err == nil {
			changedFiles[file] = content
		} else {
			fmt.Printf("⚠️ Warning: Could not read %s: %v\n", file, err)
		}
	}

	repoStructure, _ := action.GetRepoStructure()

	// 3. Prepare Context
	prContext := models.PRContext{
		Title: "Automated PR Review",
		Body:  "Running via GitHub Actions CLI",
	}

	var ghClient *action.GitHubClient
	if repoName != "" {
		parts := strings.Split(repoName, "/")
		if len(parts) == 2 {
			if appID != "" && appPrivateKey != "" {
				fmt.Println("🔑 Using GitHub App credentials...")
				appIDInt, err := strconv.ParseInt(appID, 10, 64)
				if err == nil {
					ghClient, err = action.NewGitHubAppClient(context.Background(), appIDInt, appPrivateKey, parts[0], parts[1])
					if err != nil {
						fmt.Printf("⚠️ Failed to initialize App client: %v. Falling back to Token.\n", err)
						ghClient = action.NewGitHubClient(context.Background(), githubToken, parts[0], parts[1])
					}
				} else {
					fmt.Printf("⚠️ Invalid App ID: %v. Falling back to Token.\n", err)
					ghClient = action.NewGitHubClient(context.Background(), githubToken, parts[0], parts[1])
				}
			} else if githubToken != "" {
				ghClient = action.NewGitHubClient(context.Background(), githubToken, parts[0], parts[1])
			}
		}
	}

	// Fetch real PR metadata if tokens are available
	if ghClient != nil && prNumber != "" {
		var prNum int
		fmt.Sscanf(prNumber, "%d", &prNum)
		realPR, err := ghClient.GetPullRequest(context.Background(), prNum)
		if err != nil {
			fmt.Printf("⚠️ Failed to fetch PR metadata: %v. Using defaults.\n", err)
		} else {
			prContext = *realPR
			fmt.Printf("✅ Fetched PR Context: %s\n", prContext.Title)
		}
	}

	// Phase 0/1/2/3: Multi-repo compatibility guard & execution
	var matchSummary string
	var remoteGraphs map[string]*codegraph.RemoteRepoGraph
	var remoteFetch *remotefetch.Fetcher

	multiRepoStartTime := time.Now()
	var baseTempDir string
	if checkMultiRepoAvailability(context.Background(), ghClient) {
		fmt.Println("🌐 [MultiRepo] Multi-repo review capability detected.")

		// 1. Discover accessible repositories
		fmt.Println("   🔍 Discovering accessible repositories...")
		repos, err := ghClient.ListAccessibleRepos(context.Background())
		if err != nil {
			fmt.Printf("   ⚠️  Failed to discover repos: %v (falling back to single-repo)\n", err)
		} else {
			filteredRepos := multirepo.DiscoverRepos(context.Background(), repos, repoName)
			fmt.Printf("   ✅ Found %d accessible peer repositories\n", len(filteredRepos))

			// 2. Checkout & Graph Build
			fmt.Println("   📥 Fetching lightweight graphs for peer repositories...")
			workerPool := multirepo.NewWorkerPool(5)
			remoteGraphs = make(map[string]*codegraph.RemoteRepoGraph)
			// We need a temp dir to store the cloned repos.
			baseTempDir, _ = os.MkdirTemp("", "ai-reviewer-multirepo-*")
			defer os.RemoveAll(baseTempDir)

			fetchMap := make(map[string]string) // map repoFullName -> localPath for fetcher

			checkoutPaths, err := workerPool.ProcessRepos(context.Background(), filteredRepos, ghClient.Token, baseTempDir)
			if err != nil {
				fmt.Printf("   ⚠️ Failed to process repos: %v\n", err)
			}

			for repoFullName, localPath := range checkoutPaths {
				fmt.Printf("      - Building graph for %s...\n", repoFullName)
				graph, err := codegraph.BuildRemoteGraph(context.Background(), repoFullName, localPath)
				if err != nil {
					fmt.Printf("        ⚠️ Failed to build graph for %s: %v\n", repoFullName, err)
					continue
				}
				remoteGraphs[repoFullName] = graph
				fetchMap[repoFullName] = localPath
			}

			fmt.Printf("   ✅ Built graphs for %d peer repositories\n", len(remoteGraphs))
		}
	} else {
		fmt.Println("   (Running in standard single-repo mode)")
	}

	// 4.7 Code Graph: Deterministic Context Analysis
	dependencies := make(map[string]string)
	fmt.Printf("🚀 Starting Code Graph: Analyzing %d changed files...\n", len(changedFiles))

	// Initialize Code Graph Service
	wd, _ := os.Getwd()
	cgService := codegraph.NewService(wd)
	graphDir := filepath.Join(wd, ".ai-reviewer")

	// Graph Persistence: Load → Delta Update → Save
	if !*noCache {
		cachedGraph, err := codegraph.LoadGraph(graphDir)
		if err != nil {
			fmt.Printf("⚠️ Failed to load cached graph: %v\n", err)
		}
		if cachedGraph != nil {
			// Graph found — check if delta update is needed
			currentSHA := codegraph.GetCurrentCommitSHA(wd)
			if cachedGraph.CommitSHA == currentSHA {
				fmt.Println("✅ [CodeGraph] Cache hit — graph is current")
			} else {
				fmt.Printf("🔄 [CodeGraph] Cache stale (cached: %.7s, current: %.7s) — running delta update\n",
					cachedGraph.CommitSHA, currentSHA)
				if err := codegraph.DeltaUpdate(context.Background(), cachedGraph, wd, changedFilesList); err != nil {
					fmt.Printf("⚠️ Delta update failed: %v\n", err)
				}
			}
			cgService.SetGraph(cachedGraph)
		} else {
			// No cache — build full graph
			fmt.Println("🔨 [CodeGraph] No cache found — building full graph...")
			fullGraph, err := codegraph.BuildFull(context.Background(), wd)
			if err != nil {
				fmt.Printf("⚠️ Full graph build failed: %v\n", err)
			} else {
				cgService.SetGraph(fullGraph)
			}
		}
	}

	cgContext, err := cgService.GetContext(context.Background(), changedFiles)
	if err != nil {
		fmt.Printf("⚠️ Code Graph failed: %v\n", err)
	} else {
		for path, content := range cgContext {
			dependencies[path] = content
		}
	}

	// Save graph after analysis
	if !*noCache && cgService.Graph != nil {
		if err := codegraph.SaveGraph(cgService.Graph, graphDir); err != nil {
			fmt.Printf("⚠️ Failed to save graph: %v\n", err)
		}
	}

	// Multi-Repo Matching Execution (Phase 2 & 3 combined)
	if baseTempDir != "" && ghClient != nil && cgService.Graph != nil && len(remoteGraphs) > 0 {
		fmt.Println("   🔄 Analyzing cross-repo dependencies...")
		localSignals := reposelect.ExtractLocalSignals(cgService.Graph, changedFiles)

		matches := reposelect.MatchRepos(localSignals, remoteGraphs)
		if len(matches) > 0 {
			matchSummary = reposelect.FormatMatchSummary(matches)
			fmt.Printf("   ✅ Found cross-repo references for %d peer repositories\n", len(matches))

			remoteFetch = remotefetch.NewFetcher(baseTempDir, 5, 200, 500000)

			// Because the worker pool creates temp directories, we need a way to fetch lines from them later.
			// Instead of closing the pool / directories, we'll keep them around until process exit.
		} else {
			fmt.Println("   ℹ️ No direct cross-repo references found in this PR's scope.")
		}

		fmt.Printf("⏱️  [Timing] Multi-repo enrichment completed in %v\n", time.Since(multiRepoStartTime))
	}

	// Note: Review layer selection is now handled by the multi-agent orchestrator.
	// Each specialist agent (Correctness, Security, Structure) covers its own layers.

	// 4.8 Developer Rules: Parse, sanitize, and apply ignore filter
	reviewRulesYAML := *reviewRulesFlag
	if reviewRulesYAML == "" {
		reviewRulesYAML = os.Getenv("REVIEW_RULES")
	}

	var devRules *models.DeveloperRules
	if reviewRulesYAML != "" {
		fmt.Println("📋 [DevRules] Parsing developer-defined review rules...")
		parsed, err := rules.Parse(reviewRulesYAML)
		if err != nil {
			fmt.Printf("⚠️  [DevRules] Failed to parse review_rules: %v (continuing without rules)\n", err)
		} else if parsed != nil {
			devRules = rules.Sanitize(parsed)
			fmt.Printf("✅ [DevRules] Loaded %d instructions, %d ignore patterns, %d focus areas\n",
				len(devRules.Instructions), len(devRules.Ignore), len(devRules.Focus))

			// Apply ignore filter to changed files
			if len(devRules.Ignore) > 0 {
				ignoreFilter := rules.BuildIgnoreFilter(devRules)
				filtered := make(map[string]string)
				var ignored []string
				for path, content := range changedFiles {
					if ignoreFilter(path) {
						ignored = append(ignored, path)
					} else {
						filtered[path] = content
					}
				}
				if len(ignored) > 0 {
					fmt.Printf("   🚫 [DevRules] Excluded %d files matching ignore patterns:\n", len(ignored))
					for _, f := range ignored {
						fmt.Printf("      - %s\n", f)
					}
					changedFiles = filtered
				}
			}
		}
	}

	// 4. Run Review — with token-aware chunking
	ctx := context.Background()

	var provider llm.LLMProvider
	var initErr error

	switch llmProvider {
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			key = apiKey
		}
		provider = llm.NewOpenAIProvider(key, "")
	case "claude":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			key = apiKey
		}
		provider = llm.NewClaudeProvider(key, "")
	default:
		provider, initErr = llm.NewGeminiProvider(apiKey, "")
	}

	if initErr != nil || provider == nil {
		fmt.Printf("❌ Failed to initialize provider %s: %v\n", llmProvider, initErr)
		os.Exit(1)
	}

	// Get graph edges for smart chunk grouping
	var graphEdges []codegraph.Edge
	if cgService.Graph != nil {
		graphEdges = cgService.Graph.Edges
	}

	chunks := chunker.GroupFiles(changedFiles, diff, graphEdges, chunker.DefaultTokenBudget)
	fmt.Printf("🚀 Starting Review: %d files → %d chunk(s)\n", len(changedFiles), len(chunks))

	// Detailed Chunking Breakdown Logging (enabled if multi-chunk)
	if len(chunks) > 1 {
		fmt.Println("\n═══════════════════════════════════════════════════════════════════════════════")
		fmt.Println("📦 [CodeGraph] Chunking Strategy Breakdown")
		fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
		for _, chunk := range chunks {
			fmt.Printf("Chunk %d/%d (%d files):\n", chunk.Index, chunk.Total, len(chunk.Files))
			fileList := make([]string, 0, len(chunk.Files))
			for f := range chunk.Files {
				fileList = append(fileList, filepath.Base(f))
			}
			sort.Strings(fileList)
			fmt.Printf("  Files: %s\n", strings.Join(fileList, ", "))
			if len(chunk.CrossRefs) > 0 {
				refList := make([]string, 0, len(chunk.CrossRefs))
				for _, r := range chunk.CrossRefs {
					refList = append(refList, filepath.Base(r))
				}
				fmt.Printf("  🔗 Cross-chunk refs: %s\n", strings.Join(refList, ", "))
			}
			fmt.Println()
		}
		fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	}

	var results []*models.ReviewResult
	for _, chunk := range chunks {
		// Scope dependencies to this chunk's files
		scopedDeps := scopeDependencies(dependencies, chunk, cgService)

		// Multi-agent review: 3 specialist agents in parallel per chunk
		r, err := orchestrator.ReviewChunk(ctx, provider,
			chunk.Files, chunk.Diff,
			chunk.Index, chunk.Total,
			chunk.CrossRefs,
			scopedDeps, repoStructure,
			prContext,
			cgService.Graph, wd,
			matchSummary,
			remoteGraphs,
			remoteFetch,
			devRules,
		)
		if err != nil {
			fmt.Printf("❌ Chunk %d/%d review failed: %v\n", chunk.Index, chunk.Total, err)
			continue // Don't abort — review remaining chunks
		}
		results = append(results, r)
	}

	if len(results) == 0 {
		fmt.Println("❌ All chunks failed")
		os.Exit(1)
	}

	// Cross-chunk consolidation (each chunk was already consolidated by orchestrator)
	result := llm.ConsolidateResults(results)

	// 5. Output Results
	if *dryRun || githubToken == "" {
		fmt.Printf("✅ Review Complete - Summary: %s\n", result.Summary)
		if *dryRun {
			fmt.Println("Results not posted (Dry Run)")
		}
	} else {
		// Post to GitHub
		if repoName == "" || prNumber == "" {
			fmt.Println("❌ Error: GITHUB_REPOSITORY and PR_NUMBER are required for posting comments.")
			os.Exit(1)
		}

		fmt.Printf("🚀 Posting comments to %s PR #%s...\n", repoName, prNumber)

		parts := strings.Split(repoName, "/")
		if len(parts) != 2 {
			fmt.Printf("❌ Invalid repo name format: %s\n", repoName)
			os.Exit(1)
		}

		var prNum int
		fmt.Sscanf(prNumber, "%d", &prNum)

		if ghClient == nil {
			ghClient = action.NewGitHubClient(ctx, githubToken, parts[0], parts[1])
		}
		if err := ghClient.PostReview(ctx, prNum, result, commitSHA, diff); err != nil {
			fmt.Printf("❌ Failed to post review: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Review posted successfully!")
	}
}

// scopeDependencies filters the full dependency map to only include entries
// relevant to a specific chunk's files.
//
// For single-chunk PRs: ALL deps are included (no filtering — the code graph
// already curated these as relevant).
//
// For multi-chunk PRs: deps are included if they share a directory with any
// chunk file, are cross-refs, or are compact graph summaries.
func scopeDependencies(allDeps map[string]string, chunk chunker.Chunk, cgService *codegraph.Service) map[string]string {
	if len(allDeps) == 0 {
		return allDeps
	}

	// Single-chunk PR → include ALL deps. The code graph already selected
	// only the relevant ones; filtering further drops critical context.
	if chunk.Total == 1 {
		return allDeps
	}

	// Multi-chunk: scope to this chunk's needs
	scoped := make(map[string]string)
	for depPath, content := range allDeps {
		// Always include context graph summaries (compact, universal)
		if strings.HasPrefix(content, "CONTEXT GRAPH") || strings.HasPrefix(content, "DATA FLOW") {
			scoped[depPath] = content
			continue
		}

		// Include dependency if any chunk file is in the same directory
		depDir := filepath.Dir(depPath)
		for chunkFile := range chunk.Files {
			if filepath.Dir(chunkFile) == depDir {
				scoped[depPath] = content
				break
			}
		}

		// Include if it's a cross-ref dependency
		for _, ref := range chunk.CrossRefs {
			if ref == depPath {
				scoped[depPath] = content
				break
			}
		}
	}

	// If scoping removed everything, don't fall back to all deps (which blows the budget).
	// Instead, only return the context graph summaries which are compact.
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
