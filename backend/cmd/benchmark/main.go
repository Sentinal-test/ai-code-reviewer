package main

import (
	"code-review/backend/internal/codegraph"
	"code-review/backend/internal/llm"
	"code-review/backend/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

type Truth struct {
	ID                   string    `json:"id"`
	Language             string    `json:"language"`
	Category             string    `json:"category"`
	PRContext            PRContext `json:"pr_context"`
	ExpectedDependencies []string  `json:"expected_dependencies"`
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
	CaseID           string
	Passed           bool
	Recall           float64
	Precision        float64
	DependencyRecall float64
	Comments         int
	Error            string
}

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Note: .env file not found, using system environment variables\n")
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		fmt.Println("❌ Error: GEMINI_API_KEY is required")
		os.Exit(1)
	}

	datasetRoot := "../datasets" // Relative to backend/cmd/benchmark
	absRoot, _ := filepath.Abs(datasetRoot)
	fmt.Printf("📊 Starting Benchmark Evaluation on: %s\n", absRoot)

	var results []Result

	filepath.Walk(absRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == "truth.json" {
			res := runEvalCase(path, apiKey)
			results = append(results, res)
		}
		return nil
	})

	printSummary(results)
}

func runEvalCase(truthPath, apiKey string) Result {
	caseDir := filepath.Dir(truthPath)
	caseID := filepath.Base(caseDir)

	fmt.Printf("\n🔍 Evaluating Case: %s\n", caseID)

	// 1. Load Truth
	truthData, err := os.ReadFile(truthPath)
	if err != nil {
		return Result{CaseID: caseID, Error: "Failed to read truth.json"}
	}
	var truth Truth
	if err := json.Unmarshal(truthData, &truth); err != nil {
		return Result{CaseID: caseID, Error: fmt.Sprintf("Failed to parse truth.json: %v", err)}
	}

	// 2. Load Diff
	diffPath := filepath.Join(caseDir, "diff.patch")
	diff, err := os.ReadFile(diffPath)
	if err != nil {
		return Result{CaseID: caseID, Error: "Failed to read diff.patch"}
	}

	// 3. Load Buggy Files
	buggyDir := filepath.Join(caseDir, "buggy")
	changedFiles := make(map[string]string)
	filepath.Walk(buggyDir, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			rel, _ := filepath.Rel(buggyDir, path)
			content, _ := os.ReadFile(path)
			changedFiles[rel] = string(content)
		}
		return nil
	})

	// 4. Run Review
	ctx := context.Background()
	client := &http.Client{}

	// Default Settings
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

	// EXECUTE CODE GRAPH if expected dependencies are listed
	dependencies := make(map[string]string)
	depRecall := 1.0
	if len(truth.ExpectedDependencies) > 0 {
		fmt.Printf("   🔭 Running Code Graph...\n")
		// Initialize Code Graph Service
		// Benchmark runs from backend/cmd/benchmark, so repo root is ../..
		// However, we are running on datasets which are external.
		// The Scanner expects a RepoPath to run git grep in.
		// For benchmarks, the "Changes" are in `caseDir/buggy`.
		// But definition files might be in `caseDir/deps` or implied.
		// Detailed benchmarking of Code Graph requires strict repo structure.
		// We'll point Scanner to caseDir for now.
		cgService := codegraph.NewService(caseDir)

		cgContext, _ := cgService.GetContext(ctx, changedFiles)

		foundPaths := make([]string, 0, len(cgContext))
		for p := range cgContext {
			foundPaths = append(foundPaths, p)
		}

		tpDeps := 0
		for _, expected := range truth.ExpectedDependencies {
			found := false
			for _, f := range foundPaths {
				// Benchmark truth might use relative paths, Code Graph returns relative to RepoPath
				if f == expected || strings.HasSuffix(f, expected) {
					found = true
					break
				}
			}
			if found {
				tpDeps++
				// Load actual dependency content if available in deps/ folder
				depPath := filepath.Join(caseDir, "deps", expected)
				if content, err := os.ReadFile(depPath); err == nil {
					dependencies[expected] = string(content)
				} else {
					dependencies[expected] = "// Dependency found (content missing in test case)"
				}
			}
		}
		depRecall = float64(tpDeps) / float64(len(truth.ExpectedDependencies))
		fmt.Printf("   🔍 Code Graph Recall: %.2f (%d/%d found)\n", depRecall, tpDeps, len(truth.ExpectedDependencies))
	}

	review, err := llm.RunReview(ctx, client, string(diff), changedFiles, dependencies, settings, "", apiKey, prContext)
	if err != nil {
		return Result{CaseID: caseID, Error: fmt.Sprintf("LLM Review Failed: %v", err)}
	}

	// Print Generated Comments (Restored as requested)
	fmt.Printf("   📝 Generated Comments:\n")
	for _, c := range review.Comments {
		fmt.Printf("      - [%s] %s:%d: %s\n", c.Severity, c.File, c.Line, c.Message)
	}

	// 5. Calculate Metrics
	tp := 0
	for _, bug := range truth.Bugs {
		found := false
		for _, comment := range review.Comments {
			// Basic matching logic: file and line (approximate line match +/- 5)
			if comment.File == bug.File && (comment.Line >= bug.Line-5 && comment.Line <= bug.Line+5) {
				found = true
				break
			}
		}
		if found {
			tp++
		}
	}

	recall := float64(tp) / float64(len(truth.Bugs))
	precision := 0.0
	if len(review.Comments) > 0 {
		precision = float64(tp) / float64(len(review.Comments))
	}

	passed := recall >= 1.0 && depRecall >= 0.5 // Pass if found all bugs and at least half deps

	fmt.Printf("   ✅ Recall: %.2f | Precision: %.2f | Comments: %d\n", recall, precision, len(review.Comments))

	return Result{
		CaseID:           caseID,
		Passed:           passed,
		Recall:           recall,
		Precision:        precision,
		DependencyRecall: depRecall,
		Comments:         len(review.Comments),
	}
}

func printSummary(results []Result) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("             BENCHMARK SUMMARY")
	fmt.Println(strings.Repeat("=", 60))

	total := len(results)
	passed := 0
	avgRecall := 0.0
	avgPrecision := 0.0
	avgDepRecall := 0.0

	for _, res := range results {
		status := "❌ FAIL"
		if res.Passed {
			status = "✅ PASS"
			passed++
		}
		fmt.Printf("%-10s | Recall: %.2f | Prec: %.2f | DepRec: %.2f | %s\n",
			res.CaseID, res.Recall, res.Precision, res.DependencyRecall, status)

		avgRecall += res.Recall
		avgPrecision += res.Precision
		avgDepRecall += res.DependencyRecall
	}

	if total > 0 {
		fmt.Println(strings.Repeat("-", 60))
		fmt.Printf("Total Cases: %d | Passed: %d (%.0f%%)\n", total, passed, (float64(passed)/float64(total))*100)
		fmt.Printf("Avg Recall: %.2f | Avg Precision: %.2f | Avg DepRecall: %.2f\n",
			avgRecall/float64(total), avgPrecision/float64(total), avgDepRecall/float64(total))
	}
	fmt.Println(strings.Repeat("=", 60))
}
