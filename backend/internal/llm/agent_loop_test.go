package llm

import (
	"code-review/backend/internal/agents"
	"context"
	"encoding/json"
	"errors"
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

// toolExhaustingProvider returns a tool call for the first N responses,
// then returns a final JSON text response to simulate the finalization path.
type toolExhaustingProvider struct {
	callCount    int
	toolTurns    int // how many turns to return tool calls before finalizing
	finalText    string
	requestsSeen []GenerateRequest
}

func (p *toolExhaustingProvider) GenerateContent(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	p.requestsSeen = append(p.requestsSeen, req)
	p.callCount++
	if p.callCount <= p.toolTurns {
		fc := &FunctionCall{ID: "1", Name: "get_file_content", Args: map[string]interface{}{"path": "main.go"}}
		return GenerateResponse{
			FunctionCalls: []*FunctionCall{fc},
			ModelParts:    []Part{{FunctionCall: fc}},
			FinishReason:  "STOP",
		}, nil
	}
	return GenerateResponse{
		Text:         p.finalText,
		FinishReason: "STOP",
	}, nil
}

func (p *toolExhaustingProvider) CreateCache(_ context.Context, _, _ string, _ []ToolDeclaration) (string, error) {
	return "", nil
}
func (p *toolExhaustingProvider) DeleteCache(_ context.Context, _ string) error { return nil }
func (p *toolExhaustingProvider) GetName() string                                { return "exhausting" }

func TestRunAgentReviewFinalizesAfterToolBudgetExhausted(t *testing.T) {
	finalJSON, _ := json.Marshal(map[string]any{
		"summary":     "Found 1 issue via finalization",
		"comments":    []any{map[string]any{"file": "main.go", "line": 10, "severity": "warning", "layer": "bug", "message": "nil deref"}},
		"resolutions": []any{},
	})

	provider := &toolExhaustingProvider{
		// Return tool calls for exactly maxToolIterations+1 turns (the full budget),
		// then a final JSON response on the finalization call.
		toolTurns: maxToolIterations + 1,
		finalText: string(finalJSON),
	}

	config := agents.AgentConfig{
		Type:         agents.AgentCorrectness,
		SystemPrompt: "system",
		ChangedFiles: map[string]string{"main.go": "package main\nfunc main() {}\n"},
		Diff:         "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1,2 @@\n+func main() {}\n",
	}

	result := RunAgentReview(context.Background(), provider, config, nil)

	if result.Error != nil {
		t.Fatalf("expected no error after finalization, got: %v", result.Error)
	}
	if len(result.Comments) != 1 {
		t.Fatalf("expected 1 comment from finalization, got %d", len(result.Comments))
	}
	if result.Summary != "Found 1 issue via finalization" {
		t.Fatalf("unexpected summary: %q", result.Summary)
	}

	// The last request must have no tools (finalization is tool-free)
	lastReq := provider.requestsSeen[len(provider.requestsSeen)-1]
	if len(lastReq.Tools) != 0 {
		t.Fatalf("finalization request must have no tools, got %d", len(lastReq.Tools))
	}
	if !lastReq.ResponseJSON {
		t.Fatal("finalization request must enforce ResponseJSON=true")
	}
}

func TestIsTimeoutError(t *testing.T) {
	if !isTimeoutError(context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded to be detected as timeout")
	}
	if !isTimeoutError(errors.New("Error 504, Message: Deadline expired before operation could complete., Status: DEADLINE_EXCEEDED")) {
		t.Fatalf("expected provider deadline error to be detected as timeout")
	}
	if isTimeoutError(errors.New("some other failure")) {
		t.Fatalf("did not expect non-timeout error to be detected as timeout")
	}
}
