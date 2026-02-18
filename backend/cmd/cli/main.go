package main

import (
	"code-review/backend/internal/action"
	"code-review/backend/internal/chunker"
	"code-review/backend/internal/codegraph"
	"code-review/backend/internal/llm"
	"code-review/backend/internal/models"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	// 1. Parse Args & Env
	apiKeyFlag := flag.String("api-key", "", "Gemini API Key")
	githubTokenFlag := flag.String("github-token", "", "GitHub Token")
	prNumberFlag := flag.String("pr-number", "", "Pull Request Number")
	repoNameFlag := flag.String("repo", "", "Repository Name (owner/repo)")
	commitShaFlag := flag.String("sha", "", "Commit SHA")

	// Flags for local testing or overrides
	baseRef := flag.String("base", "main", "Base ref to diff against")
	headRef := flag.String("head", "HEAD", "Head ref to diff")
	dryRun := flag.Bool("dry-run", false, "Print results to stdout instead of commenting")
	noCache := flag.Bool("no-cache", false, "Skip graph cache (force fresh analysis)")
	flag.Parse()

	// 2. Resolve parameters (Priority: Flag -> Env)
	apiKey := *apiKeyFlag
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
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

	if apiKey == "" {
		fmt.Println("❌ Error: GEMINI_API_KEY is required. Pass via --api-key or GEMINI_API_KEY environment variable.")
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

	// Fetch real PR metadata if tokens are available
	if githubToken != "" && repoName != "" && prNumber != "" {
		parts := strings.Split(repoName, "/")
		if len(parts) == 2 {
			var prNum int
			fmt.Sscanf(prNumber, "%d", &prNum)
			ghClient := action.NewGitHubClient(context.Background(), githubToken, parts[0], parts[1])
			realPR, err := ghClient.GetPullRequest(context.Background(), prNum)
			if err != nil {
				fmt.Printf("⚠️ Failed to fetch PR metadata: %v. Using defaults.\n", err)
			} else {
				prContext = *realPR
				fmt.Printf("✅ Fetched PR Context: %s\n", prContext.Title)
			}
		}
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

	// Settings - Default to "Enable All" for Action mode, or parse inputs
	settings := models.RepoSettings{
		SecurityEnabled:     true,
		BugEnabled:          true,
		LintEnabled:         true,
		PerformanceEnabled:  true,
		ArchitectureEnabled: true,
	}

	// 4. Run Review — with token-aware chunking
	ctx := context.Background()
	client := &http.Client{}

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

		r, err := llm.RunChunkReview(ctx, client,
			chunk.Index, chunk.Total,
			chunk.Files, chunk.Diff, chunk.CrossRefs,
			scopedDeps, settings, repoStructure, apiKey, prContext,
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

		ghClient := action.NewGitHubClient(ctx, githubToken, parts[0], parts[1])
		if err := ghClient.PostReview(ctx, prNum, result, commitSHA); err != nil {
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
