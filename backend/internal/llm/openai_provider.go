package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// Default OpenAI model for code reviews
	openAIModel = "gpt-4o"
	openAIURL   = "https://api.openai.com/v1/chat/completions"
)

type OpenAIProvider struct {
	httpClient *http.Client
	apiKey     string
	model      string
	baseURL    string
}

func NewOpenAIProvider(apiKey string, model string) *OpenAIProvider {
	if model == "" {
		model = openAIModel
	}
	return &OpenAIProvider{
		httpClient: &http.Client{Timeout: 120 * time.Second},
		apiKey:     apiKey,
		model:      model,
		baseURL:    openAIURL,
	}
}

func (p *OpenAIProvider) CreateCache(ctx context.Context, systemPrompt, staticContext string, tools []ToolDeclaration) (string, error) {
	// OpenAI handles prompt caching implicitly.
	return "", nil
}

func (p *OpenAIProvider) DeleteCache(ctx context.Context, cacheID string) error {
	return nil
}

type openAIChatRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Tools    []openAITool    `json:"tools,omitempty"`
}

type openAIMessage struct {
	Role         string               `json:"role"`
	Content      string               `json:"content,omitempty"`
	ToolCalls    []openAIToolCall     `json:"tool_calls,omitempty"`
	ToolCallID   string               `json:"tool_call_id,omitempty"`
	Name         string               `json:"name,omitempty"`
}

type openAITool struct {
	Type     string            `json:"type"`
	Function openAIToolDetails `json:"function"`
}

type openAIToolDetails struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openAIToolCallFunction `json:"function"`
}

type openAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message openAIMessage `json:"message"`
		Reason  string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openAIErrorEnvelope struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *OpenAIProvider) buildRequest(req GenerateRequest) (openAIChatRequest, error) {
	var messages []openAIMessage

	// 1. Add System Prompt as a developer/system message
	if req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{
			Role:    "system",
			Content: req.SystemPrompt,
		})
	}

	// 2. Add conversation history
	for _, msg := range req.Messages {
		role := msg.Role
		if role == "model" {
			role = "assistant"
		}
		
		text := collectMessageText(msg.Parts)
		
		var toolCalls []openAIToolCall
		isToolResp := false
		for _, part := range msg.Parts {
			if part.FunctionCall != nil {
				argsBytes, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, openAIToolCall{
					ID:   part.FunctionCall.ID,
					Type: "function",
					Function: openAIToolCallFunction{
						Name:      part.FunctionCall.Name,
						Arguments: string(argsBytes),
					},
				})
			}
			if part.FunctionResp != nil {
				isToolResp = true
			}
		}

		// Only append a base message if it has text, tool calls, or if it's NOT a tool response container
		if text != "" || len(toolCalls) > 0 || !isToolResp {
			messages = append(messages, openAIMessage{
				Role:      role,
				Content:   text,
				ToolCalls: toolCalls,
			})
		}
		
		// Handle function responses (OpenAI "tool" messages)
		for _, part := range msg.Parts {
			if part.FunctionResp != nil {
				messages = append(messages, openAIMessage{
					Role:       "tool",
					ToolCallID: part.FunctionResp.ID,
					Name:       part.FunctionResp.Name,
					Content:    part.FunctionResp.Content,
				})
			}
		}
	}

	apiReq := openAIChatRequest{
		Model:    p.model,
		Messages: messages,
	}

	if len(req.Tools) > 0 {
		apiReq.Tools = make([]openAITool, 0, len(req.Tools))
		for _, tool := range req.Tools {
			apiReq.Tools = append(apiReq.Tools, openAITool{
				Type: "function",
				Function: openAIToolDetails{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.Parameters,
				},
			})
		}
	}

	return apiReq, nil
}


func collectMessageText(parts []Part) string {
	var chunks []string
	for _, part := range parts {
		if part.Text != "" {
			chunks = append(chunks, part.Text)
		}
	}
	return strings.Join(chunks, "\n")
}

func (p *OpenAIProvider) GenerateContent(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	apiReq, err := p.buildRequest(req)
	if err != nil {
		return GenerateResponse{}, err
	}

	body, err := json.Marshal(apiReq)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(body))
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("create openai request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("openai error: %w", err)
	}
	defer httpResp.Body.Close()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("read openai response: %w", err)
	}

	if httpResp.StatusCode >= 300 {
		var apiErr openAIErrorEnvelope
		if err := json.Unmarshal(respBytes, &apiErr); err == nil && apiErr.Error != nil && apiErr.Error.Message != "" {
			return GenerateResponse{}, fmt.Errorf("openai error: %s", apiErr.Error.Message)
		}
		return GenerateResponse{}, fmt.Errorf("openai error: status %d: %s", httpResp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var resp openAIChatResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return GenerateResponse{}, fmt.Errorf("decode openai response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return GenerateResponse{FinishReason: "EMPTY"}, nil
	}

	choice := resp.Choices[0]
	result := GenerateResponse{
		Text:         choice.Message.Content,
		FinishReason: strings.ToUpper(choice.Reason),
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}

	for _, tc := range choice.Message.ToolCalls {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			args = make(map[string]interface{})
		}
		fc := &FunctionCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: args,
		}
		result.FunctionCalls = append(result.FunctionCalls, fc)
		result.ModelParts = append(result.ModelParts, Part{FunctionCall: fc})
	}

	return result, nil
}
