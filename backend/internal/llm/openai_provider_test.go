package llm

import "testing"

func TestBuildRequest_UsesPreviousResponseIDForToolContinuation(t *testing.T) {
	provider := &OpenAIProvider{model: "gpt-5.4"}
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
						ID:         "call_123",
						Name:       "search_codebase",
						Args:       map[string]interface{}{"query": "foo"},
						ResponseID: "resp_123",
					},
				}},
			},
			{
				Role: "function",
				Parts: []Part{{
					FunctionResp: &FunctionResponse{
						ID:      "call_123",
						Name:    "search_codebase",
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
	if apiReq.PreviousResponseID != "resp_123" {
		t.Fatalf("expected previous_response_id resp_123, got %q", apiReq.PreviousResponseID)
	}
	if len(apiReq.Input) != 1 {
		t.Fatalf("expected only function output delta input, got %d items", len(apiReq.Input))
	}
	if apiReq.Input[0]["type"] != "function_call_output" {
		t.Fatalf("expected function_call_output item, got %#v", apiReq.Input[0])
	}
}

func TestBuildRequest_UsesFullConversationWithoutPreviousResponseID(t *testing.T) {
	provider := &OpenAIProvider{model: "gpt-5.4"}
	req := GenerateRequest{
		SystemPrompt: "system",
		Messages: []Message{
			{
				Role:  "user",
				Parts: []Part{{Text: "inspect diff"}},
			},
		},
		ResponseJSON: true,
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
	if apiReq.PreviousResponseID != "" {
		t.Fatalf("expected empty previous_response_id, got %q", apiReq.PreviousResponseID)
	}
	if len(apiReq.Input) != 1 {
		t.Fatalf("expected full input, got %d items", len(apiReq.Input))
	}
	if apiReq.Model != "gpt-5.4" {
		t.Fatalf("expected model gpt-5.4, got %q", apiReq.Model)
	}
	if apiReq.Reasoning == nil || apiReq.Reasoning.Effort != "high" {
		t.Fatalf("expected reasoning effort high, got %#v", apiReq.Reasoning)
	}
	if apiReq.Text == nil || apiReq.Text.Format == nil || apiReq.Text.Format.Type != "json_object" {
		t.Fatalf("expected json_object text format, got %#v", apiReq.Text)
	}
	if len(apiReq.Tools) != 1 || apiReq.Tools[0].Type != "function" {
		t.Fatalf("expected function tool declaration, got %#v", apiReq.Tools)
	}
}
