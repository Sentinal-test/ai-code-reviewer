package orchestrator

import (
	"code-review/backend/internal/agents"
	"code-review/backend/internal/codegraph"
	"code-review/backend/internal/llm"
	"code-review/backend/internal/models"
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// isDocOrConfigFile checks if a file is documentation/config and shouldn't be reviewed.
func isDocOrConfigFile(path string) bool {
	lower := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(path))

	// Skip by extension
	skipExts := []string{".md", ".txt", ".rst", ".adoc", ".gitignore", ".dockerignore"}
	for _, ext := range skipExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}

	// Skip known config files
	skipFiles := []string{
		"go.mod", "go.sum", "package.json", "package-lock.json",
		"yarn.lock", "pnpm-lock.yaml", "tsconfig.json",
		"license", "changelog", "readme",
	}
	baseNoExt := strings.TrimSuffix(base, filepath.Ext(base))
	for _, skip := range skipFiles {
		if base == skip || baseNoExt == skip {
			return true
		}
	}

	return false
}

// filterReviewableFiles removes documentation and config files from the map.
func filterReviewableFiles(files map[string]string) map[string]string {
	filtered := make(map[string]string)
	for path, content := range files {
		if !isDocOrConfigFile(path) {
			filtered[path] = content
		}
	}
	return filtered
}

// ReviewChunk runs 3 specialist agents in parallel on a single chunk,
// then consolidates their results using LLM-based consolidation.
func ReviewChunk(
	ctx context.Context,
	client *http.Client,
	chunkFiles map[string]string,
	chunkDiff string,
	chunkIndex, chunkTotal int,
	crossRefs []string,
	dependencies map[string]string,
	repoStructure string,
	apiKey string,
	prContext models.PRContext,
	graph *codegraph.Graph,
	repoPath string,
) (*models.ReviewResult, error) {

	start := time.Now()

	// Filter out documentation and config files — agents should only review code
	reviewableFiles := filterReviewableFiles(chunkFiles)
	if len(reviewableFiles) == 0 {
		return &models.ReviewResult{
			Summary: "No reviewable code files in this chunk (only documentation/config files)",
		}, nil
	}

	if len(reviewableFiles) < len(chunkFiles) {
		fmt.Printf("  📄 [Orchestrator] Filtered %d non-code files (kept %d reviewable)\n",
			len(chunkFiles)-len(reviewableFiles), len(reviewableFiles))
	}

	// Build SLIM dependency index for Correctness and Security agents.
	slimDeps := codegraph.BuildSlimDependencyIndex(dependencies)

	// Logging: Show exactly what was built for the code-graph context
	fmt.Printf("\n🔍 [CodeGraph] Differentiated Context Summary:\n")
	fmt.Printf("   ├─ Full Dependencies: %d files\n", len(dependencies))
	fmt.Printf("   └─ Slim Index: %d entries\n", len(slimDeps))
	for k, v := range slimDeps {
		if strings.HasPrefix(k, "_codegraph/") {
			fmt.Printf("      📎 %s (%d chars)\n", k, len(v))
			if k == "_codegraph/dependency_index" {
				// Show the first few lines of the index
				lines := strings.Split(v, "\n")
				for i, line := range lines {
					if i > 5 {
						fmt.Printf("         ... (%d more lines)\n", len(lines)-i)
						break
					}
					fmt.Printf("         %s\n", line)
				}
			}
		}
	}

	// Build shared fields
	shared := agents.AgentConfig{
		ChangedFiles: reviewableFiles,
		Diff:         chunkDiff,
		PRContext:    prContext,
		CrossRefs:    crossRefs,
		ChunkIndex:   chunkIndex,
		ChunkTotal:   chunkTotal,
		APIKey:       apiKey,
		RepoPath:     repoPath,
	}

	// Build per-agent configs with DIFFERENTIATED context:
	//   🐛 Correctness: slim deps (encourages tool usage), NO repo structure
	//   🔐 Security:    slim deps (encourages tool usage), NO repo structure
	//   🏗️ Structure:   NO deps (uses tools), FULL repo structure
	correctnessConfig := shared
	correctnessConfig.Dependencies = slimDeps
	correctnessConfig.RepoStructure = "" // Correctness doesn't need repo tree

	securityConfig := shared
	securityConfig.Dependencies = slimDeps
	securityConfig.RepoStructure = "" // Security doesn't need repo tree

	structureConfig := shared
	structureConfig.Dependencies = nil            // Structure uses tools for code details
	structureConfig.RepoStructure = repoStructure // Structure needs the repo tree

	// Create tool executor for agent tool calls
	toolExecutor := llm.NewToolExecutor(repoPath, graph)

	// Prepare the 3 specialist configs
	configs := []agents.AgentConfig{
		agents.CorrectnessAgent(correctnessConfig),
		agents.SecurityAgent(securityConfig),
		agents.StructureAgent(structureConfig),
	}

	fmt.Printf("\n🚀 [Orchestrator] Chunk %d/%d — launching 3 specialist agents in parallel\n", chunkIndex, chunkTotal)
	fmt.Printf("   📁 Files: %d | 🔗 Full Deps: %d | 📝 Diff: %d chars\n",
		len(chunkFiles), len(dependencies), len(chunkDiff))
	fmt.Printf("   🐛 Correctness: slim deps (%d entries) | 🔐 Security: slim deps (%d entries) | 🏗️ Structure: repo tree (%d chars)\n",
		len(slimDeps), len(slimDeps), len(repoStructure))

	// Fan-out: launch all 3 agents in parallel goroutines
	results := make([]agents.AgentResult, len(configs))
	var wg sync.WaitGroup

	for i, config := range configs {
		wg.Add(1)
		go func(idx int, cfg agents.AgentConfig) {
			defer wg.Done()
			results[idx] = llm.RunAgentReview(ctx, client, cfg, toolExecutor)
		}(i, config)
	}

	// Wait for all agents to complete
	wg.Wait()

	elapsed := time.Since(start)

	// Log per-agent results
	totalComments := 0
	totalToolCalls := 0
	for _, r := range results {
		if r.Error != nil {
			fmt.Printf("   ❌ [%s] Error: %v\n", r.Agent, r.Error)
		} else {
			fmt.Printf("   ✅ [%s] %d comments, %d tool calls\n", r.Agent, len(r.Comments), r.ToolCalls)
			totalComments += len(r.Comments)
			totalToolCalls += r.ToolCalls
		}
	}

	fmt.Printf("🔄 [Orchestrator] Consolidating %d comments from 3 agents (%.1fs total, %d tool calls)\n",
		totalComments, elapsed.Seconds(), totalToolCalls)

	// LLM-based consolidation (falls back to deterministic on failure)
	consolidated := llm.RunConsolidation(ctx, client, results, agents.DefaultMaxComments, apiKey)

	fmt.Printf("✅ [Orchestrator] Final: %d comments after consolidation\n", len(consolidated.Comments))

	return consolidated, nil
}
