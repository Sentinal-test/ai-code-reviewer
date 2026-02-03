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
	prNumber := os.Getenv("PR_NUMBER")
	githubEventPath := os.Getenv("GITHUB_EVENT_PATH")
	repoName := os.Getenv("GITHUB_REPOSITORY") // owner/repo
	commitSHA := os.Getenv("GITHUB_SHA")

	// Flags for local testing or overrides
	baseRef := flag.String("base", "main", "Base ref to diff against")
	headRef := flag.String("head", "HEAD", "Head ref to diff")
	dryRun := flag.Bool("dry-run", false, "Print results to stdout instead of commenting")
	allowedDomain := flag.String("allowed-domain", "", "Restrict reviews to PR authors with this email domain (e.g. appointy.com)")
	flag.Parse()

	// Try to detect PR number from GitHub Event JSON if not provided
	if prNumber == "" && githubEventPath != "" {
		fmt.Printf("🔍 PR_NUMBER not provided, attempting to detect from event payload...\n")
		data, err := os.ReadFile(githubEventPath)
		if err == nil {
			// Very simple hunt for "number": 123
			// In pull_request events, this is at the top level
			importStr := string(data)
			// We look for "number": <digits>
			// This is a bit hacky but avoids heavy json parsing of unknown event types
			// In PR event it looks like: "number": 5,
			if strings.Contains(importStr, "\"pull_request\"") {
				// PR event
				// We'll search for "number":
				parts := strings.Split(importStr, "\"number\":")
				if len(parts) > 1 {
					sub := strings.TrimSpace(parts[1])
					end := strings.IndexFunc(sub, func(r rune) bool {
						return r < '0' || r > '9'
					})
					if end > 0 {
						prNumber = sub[:end]
						fmt.Printf("✅ Detected PR #%s from event payload.\n", prNumber)
					}
				}
			}
		}
	}

	if apiKey == "" {
		fmt.Println("❌ Error: GEMINI_API_KEY is required")
		os.Exit(1)
	}

	// 2. Prepare GitHub Client if needed for metadata checks
	var ghClient *action.GitHubClient
	ctx := context.Background()

	if (githubToken != "" && repoName != "" && prNumber != "") || *allowedDomain != "" {
		parts := strings.Split(repoName, "/")
		if len(parts) == 2 {
			ghClient = action.NewGitHubClient(ctx, githubToken, parts[0], parts[1])
		}
	}

	// 2.1 Domain Restriction Check
	if *allowedDomain != "" {
		if prNumber == "" || prNumber == "0" {
			fmt.Printf("⚠️ Warning: allowed-domain check requested but PR number is missing. Skipping auth check.\n")
		} else if ghClient != nil {
			var prNum int
			fmt.Sscanf(prNumber, "%d", &prNum)

			fmt.Printf("🔒 Checking authorization for PR #%d AUTHOR...\n", prNum)
			login, email, err := ghClient.GetPullRequestAuthor(ctx, prNum)
			if err != nil {
				fmt.Printf("⚠️ Warning: Could not verify PR author: %v. Proceeding with caution.\n", err)
			} else {
				fmt.Printf("👤 PR Author: %s (Email: %s)\n", login, email)
				if email == "" {
					fmt.Printf("🛑 Authorization Failed: Could not find email for user %s. Private emails might be hidden.\n", login)
					os.Exit(0) // Exit gracefully so the CI doesn't fail, but skip review
				}
				if !strings.HasSuffix(strings.ToLower(email), "@"+strings.ToLower(*allowedDomain)) {
					fmt.Printf("🛑 Authorization Failed: User %s with email %s is not from domain %s. Skipping review.\n", login, email, *allowedDomain)
					os.Exit(0)
				}
				fmt.Printf("✅ Authorization Success: User is from %s\n", *allowedDomain)
			}
		}
	}

	// 2.2 Base/Head Detection for GitHub Actions
	if githubEventPath != "" {
		data, err := os.ReadFile(githubEventPath)
		if err == nil {
			importStr := string(data)
			if strings.Contains(importStr, "\"pull_request\"") {
				// Base SHA detection
				if *baseRef == "main" || *baseRef == "origin/main" {
					parts := strings.Split(importStr, "\"base\":")
					if len(parts) > 1 {
						shaParts := strings.Split(parts[1], "\"sha\":")
						if len(shaParts) > 1 {
							val := strings.Trim(strings.Split(shaParts[1], ",")[0], " \"\n\r\t")
							if len(val) > 7 {
								*baseRef = val
								fmt.Printf("🎯 Auto-detected PR Base SHA: %s\n", *baseRef)
							}
						}
					}
				}
				// Head SHA detection
				if *headRef == "HEAD" {
					parts := strings.Split(importStr, "\"head\":")
					if len(parts) > 1 {
						shaParts := strings.Split(parts[1], "\"sha\":")
						if len(shaParts) > 1 {
							val := strings.Trim(strings.Split(shaParts[1], ",")[0], " \"\n\r\t")
							if len(val) > 7 {
								*headRef = val
								fmt.Printf("🎯 Auto-detected PR Head SHA: %s\n", *headRef)
							}
						}
					}
				}
			}
		}
	}

	// 2. Local Git Operations (Security-First: Code stays here)
	fmt.Printf("📂 Fetching diff between %s and %s...\n", *baseRef, *headRef)
	diff, err := action.GetDiff(*baseRef, *headRef)
	if err != nil {
		fmt.Printf("❌ Error getting diff: %v\n", err)
		os.Exit(1)
	}

	if len(diff) == 0 {
		fmt.Println("✅ No changes detected. (Try running on a Pull Request event)")
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
	// Re-using ctx from above
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

		// Re-using ghClient if available, otherwise create it
		if ghClient == nil {
			ghClient = action.NewGitHubClient(ctx, githubToken, parts[0], parts[1])
		}
		if err := ghClient.PostReview(ctx, prNum, result, commitSHA); err != nil {
			fmt.Printf("❌ Failed to post review: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Review posted successfully!")
	}
}
