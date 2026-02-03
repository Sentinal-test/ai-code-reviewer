package main

import (
	"code-review/backend/internal/action"
	"code-review/backend/internal/llm"
	"code-review/backend/internal/models"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
)

func main() {
	// 1. Parse Args & Env
	apiKey := os.Getenv("GEMINI_API_KEY")
	githubToken := os.Getenv("GITHUB_TOKEN")
	prNumber := os.Getenv("PR_NUMBER")         // PR number usually comes from event payload, but direct env var is simpler for some setups
	repoName := os.Getenv("GITHUB_REPOSITORY") // owner/repo
	commitSHA := os.Getenv("GITHUB_SHA")

	// Flags for local testing or overrides
	baseRef := flag.String("base", "main", "Base ref to diff against")
	headRef := flag.String("head", "HEAD", "Head ref to diff")
	dryRun := flag.Bool("dry-run", false, "Print results to stdout instead of commenting")
	flag.Parse()

	if apiKey == "" {
		fmt.Println("❌ Error: GEMINI_API_KEY is required")
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
	// In a real action, we might read the PullRequest Event JSON to get Title/Body.
	// For now, we use placeholders or minimal info.
	prContext := models.PRContext{
		Title: "Automated PR Review",
		Body:  "Running via GitHub Actions CLI",
	}

	// 4.7 Scout Pass: Analyze Dependencies
	dependencies := make(map[string]string)
	fmt.Printf("🕵️‍♀️ Scout Pass: Analyzing dependency needs...\n")

	// Create a client for the Scout Pass
	scoutClient := &http.Client{}

	depPaths, err := llm.AnalyzeDependencyNeeds(context.Background(), scoutClient, diff, changedFiles, repoStructure, prContext, apiKey)
	if err != nil {
		fmt.Printf("⚠️ Scout Pass failed: %v\n", err)
	} else {
		fmt.Printf("🔍 Scout identified %d dependencies: %v\n", len(depPaths), depPaths)
		for _, path := range depPaths {
			// Skip if already in changedFiles
			if _, exists := changedFiles[path]; exists {
				continue
			}
			fmt.Printf("  📥 Fetching dependency: %s\n", path)
			content, err := action.GetFileContent(path)
			if err != nil {
				fmt.Printf("  ⚠️ Failed to fetch dependency %s: %v\n", path, err)
			} else {
				dependencies[path] = content
			}
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

	// 4. Run Review
	fmt.Println("🤖 Starting AI Review...")
	// Action mode execution
	ctx := context.Background()
	client := &http.Client{} // Standard client

	result, err := llm.RunReview(ctx, client, diff, changedFiles, dependencies, settings, repoStructure, apiKey, prContext)
	if err != nil {
		fmt.Printf("❌ Review failed: %v\n", err)
		os.Exit(1)
	}

	// 5. Output Results
	if *dryRun || githubToken == "" {
		fmt.Println("✅ Dry Run / No Token - Printing results to stdout:")
		fmt.Println("==================================================")
		fmt.Printf("SUMMARY: %s\n", result.Summary)
		fmt.Println("--------------------------------------------------")
		for _, c := range result.Comments {
			fmt.Printf("[%s] %s:%d - %s\n", c.Severity, c.File, c.Line, c.Message)
		}
		fmt.Println("==================================================")
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
