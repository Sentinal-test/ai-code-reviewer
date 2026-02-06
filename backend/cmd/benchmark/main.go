package main

import (
	"code-review/backend/internal/analysis"
	"code-review/backend/internal/llm"
	"code-review/backend/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time" // Added for context.WithTimeout

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
		fmt.Println("GEMINI_API_KEY not set")
		return
	}

	caseFilter := os.Getenv("CASE_FILTER")

	datasetRoot := "../datasets" // Relative to backend/cmd/benchmark
	absRoot, _ := filepath.Abs(datasetRoot)
	fmt.Printf("📊 Starting Benchmark Evaluation on: %s\n", absRoot)

	var results []Result
	var cases []string
	baseDir := absRoot // Use absRoot as the base directory for walking

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only look for truth.json files
		if !info.IsDir() && info.Name() == "truth.json" {
			// Speed optimization: filter for Go and Python only
			rel, _ := filepath.Rel(baseDir, path)
			parts := strings.Split(rel, string(os.PathSeparator))
			if len(parts) > 0 && (parts[0] == "go" || parts[0] == "python") {
				caseDir := filepath.Dir(path)
				if caseFilter != "" && !strings.Contains(caseDir, caseFilter) {
					return nil
				}
				cases = append(cases, caseDir)
			}
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Error walking dataset directory: %v\n", err)
		os.Exit(1)
	}

	for _, caseDir := range cases {
		truthPath := filepath.Join(caseDir, "truth.json")
		res := runEvalCase(truthPath, apiKey)
		results = append(results, res)
	}

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
	changedFiles := make(map[string]string)
	buggyDir := filepath.Join(caseDir, "buggy")
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

	// EXECUTE DETERMINISTIC SCOUT PASS (Code Graph)
	ctxBuild, cancelBuild := context.WithTimeout(ctx, 30*time.Second)
	defer cancelBuild()

	idx := analysis.NewIndex()
	// Build index of the entire "buggy" directory (simulated repo)
	// buggyDir is already defined above
	if err := idx.BuildIndex(ctxBuild, buggyDir); err != nil {
		return Result{CaseID: caseID, Error: fmt.Sprintf("Failed to build index: %v", err)}
	}
	// Also index the "deps" folder if it exists
	depsDir := filepath.Join(caseDir, "deps")
	if _, err := os.Stat(depsDir); err == nil {
		idx.BuildIndex(ctxBuild, depsDir)
	}

	fmt.Printf("   🔭 Running Code Graph Scout...\n")
	dependencies := idx.GetRequiredContext(string(diff), prContext.Title, prContext.Body)

	tpDeps := 0
	foundFiles := []string{}
	for path := range dependencies {
		foundFiles = append(foundFiles, path)
	}

	for _, expected := range truth.ExpectedDependencies {
		found := false
		for _, f := range foundFiles {
			// Path matching might be tricky due to relative paths
			if strings.HasSuffix(f, expected) {
				found = true
				break
			}
		}
		if found {
			tpDeps++
		}
	}

	depRecall := 1.0
	if len(truth.ExpectedDependencies) > 0 {
		depRecall = float64(tpDeps) / float64(len(truth.ExpectedDependencies))
	}
	fmt.Printf("   🔍 Scout Recall: %.2f (%d/%d found)\n", depRecall, tpDeps, len(truth.ExpectedDependencies))

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
