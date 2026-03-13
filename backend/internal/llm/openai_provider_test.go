package llm

import "testing"

func TestBuildRequest_MapsRolesAndToolsCorrectly(t *testing.T) {
	provider := &OpenAIProvider{model: "gpt-4o"}
	req := GenerateRequest{
		SystemPrompt: "system",
		Messages: []Message{
			{
				Role:  "user",
				Parts: []Part{{Text: "inspect diff"}},
			},
			{
				Role: "model",
				Parts: []Part{{
					FunctionCall: &FunctionCall{
						ID:   "call_123",
						Name: "search_codebase",
						Args: map[string]interface{}{"query": "foo"},
					},
				}},
			},
			{
				Role: "function",
				Parts: []Part{{
					FunctionResp: &FunctionResponse{
						ID:   "call_123",
						Name: "search_codebase",
						Content: "match",
					},
				}},
			},
		},
	}

	apiReq, err := provider.buildRequest(req)
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}
	
	// system (1) + user (1) + assistant (1) + tool (1) = 4 messages
	if len(apiReq.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(apiReq.Messages))
	}

	if apiReq.Messages[0].Role != "system" {
		t.Errorf("expected msg 0 role system, got %s", apiReq.Messages[0].Role)
	}
	if apiReq.Messages[1].Role != "user" {
		t.Errorf("expected msg 1 role user, got %s", apiReq.Messages[1].Role)
	}
	if apiReq.Messages[2].Role != "assistant" {
		t.Errorf("expected msg 2 role assistant, got %s", apiReq.Messages[2].Role)
	}
	if apiReq.Messages[3].Role != "tool" {
		t.Errorf("expected msg 3 role tool, got %s", apiReq.Messages[3].Role)
	}
}

func TestBuildRequest_IncludesToolsInPayload(t *testing.T) {
	provider := &OpenAIProvider{model: "gpt-4o"}
	req := GenerateRequest{
		SystemPrompt: "system",
		Messages: []Message{
			{
				Role:  "user",
				Parts: []Part{{Text: "inspect diff"}},
			},
		},
		Tools: []ToolDeclaration{{
			Name:        "search_codebase",
			Description: "search",
			Parameters:  map[string]interface{}{"type": "object"},
		}},
	}

	apiReq, err := provider.buildRequest(req)
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}
	
	if len(apiReq.Tools) != 1 || apiReq.Tools[0].Function.Name != "search_codebase" {
		t.Fatalf("expected search_codebase tool, got %#v", apiReq.Tools)
	}
	if apiReq.Model != "gpt-4o" {
		t.Fatalf("expected model gpt-4o, got %q", apiReq.Model)
	}
}
