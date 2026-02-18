package orchestrator

import (
	"code-review/backend/internal/agents"
	"code-review/backend/internal/codegraph"
	"code-review/backend/internal/llm"
	"code-review/backend/internal/models"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ReviewChunk runs 3 specialist agents in parallel on a single chunk,
// then consolidates their results.
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

	// Build the base config shared by all agents
	base := agents.AgentConfig{
		ChangedFiles:  chunkFiles,
		Diff:          chunkDiff,
		Dependencies:  dependencies,
		RepoStructure: repoStructure,
		PRContext:     prContext,
		CrossRefs:     crossRefs,
		ChunkIndex:    chunkIndex,
		ChunkTotal:    chunkTotal,
		APIKey:        apiKey,
		RepoPath:      repoPath,
	}

	// Create tool executor for agent tool calls
	toolExecutor := llm.NewToolExecutor(repoPath, graph)

	// Prepare the 3 specialist configs
	configs := []agents.AgentConfig{
		agents.CorrectnessAgent(base),
		agents.SecurityAgent(base),
		agents.StructureAgent(base),
	}

	fmt.Printf("\n🚀 [Orchestrator] Chunk %d/%d — launching 3 specialist agents in parallel\n", chunkIndex, chunkTotal)
	fmt.Printf("   📁 Files: %d | 🔗 Dependencies: %d | 📝 Diff: %d chars\n",
		len(chunkFiles), len(dependencies), len(chunkDiff))

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

	// Consolidate all agent results
	consolidated := agents.Consolidate(results, agents.DefaultMaxComments)

	fmt.Printf("✅ [Orchestrator] Final: %d comments after dedup + cap\n", len(consolidated.Comments))

	return consolidated, nil
}
