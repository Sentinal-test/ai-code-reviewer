package llm

import (
	"code-review/backend/internal/agents"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type recordingProvider struct {
	createCacheInputs []string
	requests          []GenerateRequest
}

func (p *recordingProvider) GenerateContent(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	p.requests = append(p.requests, req)
	resp, _ := json.Marshal(map[string]any{
		"summary":     "No issues found",
		"comments":    []any{},
		"resolutions": []any{},
	})
	return GenerateResponse{
		Text:         string(resp),
		FinishReason: "STOP",
	}, nil
}

func (p *recordingProvider) CreateCache(ctx context.Context, systemPrompt, staticContext string, tools []ToolDeclaration) (string, error) {
	p.createCacheInputs = append(p.createCacheInputs, staticContext)
	return "cache-1", nil
}

func (p *recordingProvider) DeleteCache(ctx context.Context, cacheID string) error {
	return nil
}

func (p *recordingProvider) GetName() string {
	return "recording"
}

func TestRunAgentReviewCachesFullPromptWhenItIsLarge(t *testing.T) {
	provider := &recordingProvider{}

	var diff strings.Builder
	diff.WriteString("diff --git a/huge.go b/huge.go\n")
	diff.WriteString("--- a/huge.go\n")
	diff.WriteString("+++ b/huge.go\n")
	diff.WriteString("@@ -1,0 +1,6000 @@\n")
	for i := 0; i < 6000; i++ {
		diff.WriteString("+very_long_added_line_to_force_a_large_dynamic_prompt_and_trigger_full_prompt_caching\n")
	}

	config := agents.AgentConfig{
		Type:         agents.AgentCorrectness,
		SystemPrompt: "system",
		ChangedFiles: map[string]string{
			"huge.go": "package demo\nfunc review() {}\n",
		},
		Diff: diff.String(),
	}

	result := RunAgentReview(context.Background(), provider, config, nil)
	if result.Error != nil {
		t.Fatalf("unexpected review error: %v", result.Error)
	}
	if len(provider.createCacheInputs) != 1 {
		t.Fatalf("expected one cache creation, got %d", len(provider.createCacheInputs))
	}
	if !strings.Contains(provider.createCacheInputs[0], "ACTIONABLE TARGET: Changed Code Files") {
		t.Fatalf("expected cached content to include the full review prompt")
	}
	if len(provider.requests) != 1 {
		t.Fatalf("expected one generate request, got %d", len(provider.requests))
	}
	if provider.requests[0].CachedContent == "" {
		t.Fatalf("expected generate request to use cached content")
	}
	gotPrompt := provider.requests[0].Messages[0].Parts[0].Text
	if strings.Contains(gotPrompt, "ACTIONABLE TARGET: Changed Code Files") {
		t.Fatalf("expected runtime prompt to be lightweight when full prompt is cached")
	}
	if !strings.Contains(gotPrompt, "Review the cached code changes now") {
		t.Fatalf("expected cached review instruction in runtime prompt, got %q", gotPrompt)
	}
}
