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
	openAIModel        = "gpt-5.4"
	openAIResponsesURL = "https://api.openai.com/v1/responses"
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
		baseURL:    openAIResponsesURL,
	}
}

func (p *OpenAIProvider) CreateCache(ctx context.Context, systemPrompt, staticContext string, tools []ToolDeclaration) (string, error) {
	// OpenAI handles prompt caching implicitly.
	return "", nil
}

func (p *OpenAIProvider) DeleteCache(ctx context.Context, cacheID string) error {
	return nil
}

type openAIResponsesRequest struct {
	Model              string                 `json:"model"`
	Instructions       string                 `json:"instructions,omitempty"`
	Input              []map[string]any       `json:"input,omitempty"`
	PreviousResponseID string                 `json:"previous_response_id,omitempty"`
	Tools              []openAIResponsesTool  `json:"tools,omitempty"`
	Reasoning          *openAIReasoningConfig `json:"reasoning,omitempty"`
	Text               *openAITextConfig      `json:"text,omitempty"`
	MaxOutputTokens    int                    `json:"max_output_tokens,omitempty"`
}

type openAIResponsesTool struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	Strict      bool                   `json:"strict"`
}

type openAIReasoningConfig struct {
	Effort string `json:"effort,omitempty"`
}

type openAITextConfig struct {
	Format *openAITextFormat `json:"format,omitempty"`
}

type openAITextFormat struct {
	Type string `json:"type"`
}

type openAIResponsesResponse struct {
	ID                string                      `json:"id"`
	Status            string                      `json:"status"`
	Output            []openAIResponsesOutputItem `json:"output"`
	IncompleteDetails *openAIIncompleteDetails    `json:"incomplete_details,omitempty"`
	Usage             *openAIResponsesUsage       `json:"usage,omitempty"`
}

type openAIResponsesOutputItem struct {
	Type      string                   `json:"type"`
	ID        string                   `json:"id,omitempty"`
	CallID    string                   `json:"call_id,omitempty"`
	Name      string                   `json:"name,omitempty"`
	Arguments string                   `json:"arguments,omitempty"`
	Content   []openAIResponsesContent `json:"content,omitempty"`
}

type openAIResponsesContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type openAIIncompleteDetails struct {
	Reason string `json:"reason,omitempty"`
}

type openAIResponsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type openAIErrorEnvelope struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *OpenAIProvider) buildRequest(req GenerateRequest) (openAIResponsesRequest, error) {
	previousResponseID, startIdx := findPreviousOpenAIResponse(req.Messages)
	input, err := buildOpenAIInputItems(req.Messages, startIdx)
	if err != nil {
		return openAIResponsesRequest{}, err
	}

	apiReq := openAIResponsesRequest{
		Model:           p.model,
		Instructions:    req.SystemPrompt,
		Input:           input,
		MaxOutputTokens: 8192,
		Reasoning: &openAIReasoningConfig{
			Effort: "high",
		},
	}

	if previousResponseID != "" {
		apiReq.PreviousResponseID = previousResponseID
	}

	if req.ResponseJSON {
		apiReq.Text = &openAITextConfig{
			Format: &openAITextFormat{Type: "json_object"},
		}
	}

	if len(req.Tools) > 0 {
		apiReq.Tools = make([]openAIResponsesTool, 0, len(req.Tools))
		for _, tool := range req.Tools {
			apiReq.Tools = append(apiReq.Tools, openAIResponsesTool{
				Type:        "function",
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
				Strict:      false,
			})
		}
	}

	return apiReq, nil
}

func findPreviousOpenAIResponse(messages []Message) (string, int) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "model" {
			continue
		}
		for _, part := range messages[i].Parts {
			if part.FunctionCall != nil && part.FunctionCall.ResponseID != "" {
				return part.FunctionCall.ResponseID, i + 1
			}
		}
	}
	return "", 0
}

func buildOpenAIInputItems(messages []Message, startIdx int) ([]map[string]any, error) {
	var input []map[string]any

	for i := startIdx; i < len(messages); i++ {
		items, err := buildOpenAIMessageItems(messages[i])
		if err != nil {
			return nil, err
		}
		input = append(input, items...)
	}

	return input, nil
}

func buildOpenAIMessageItems(message Message) ([]map[string]any, error) {
	switch message.Role {
	case "user":
		text := collectMessageText(message.Parts)
		if text == "" {
			return nil, nil
		}
		return []map[string]any{
			{
				"role":    "user",
				"content": text,
			},
		}, nil
	case "model":
		var items []map[string]any
		text := collectMessageText(message.Parts)
		if text != "" {
			items = append(items, map[string]any{
				"role":    "assistant",
				"content": text,
			})
		}
		for _, part := range message.Parts {
			if part.FunctionCall == nil {
				continue
			}
			argsBytes, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				return nil, fmt.Errorf("marshal openai function args: %w", err)
			}
			items = append(items, map[string]any{
				"type":      "function_call",
				"call_id":   part.FunctionCall.ID,
				"name":      part.FunctionCall.Name,
				"arguments": string(argsBytes),
			})
		}
		return items, nil
	case "function":
		var items []map[string]any
		for _, part := range message.Parts {
			if part.FunctionResp == nil {
				continue
			}
			items = append(items, map[string]any{
				"type":    "function_call_output",
				"call_id": part.FunctionResp.ID,
				"output":  part.FunctionResp.Content,
			})
		}
		return items, nil
	default:
		text := collectMessageText(message.Parts)
		if text == "" {
			return nil, nil
		}
		return []map[string]any{
			{
				"role":    message.Role,
				"content": text,
			},
		}, nil
	}
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

	var resp openAIResponsesResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return GenerateResponse{}, fmt.Errorf("decode openai response: %w", err)
	}

	if len(resp.Output) == 0 {
		return GenerateResponse{FinishReason: "EMPTY"}, nil
	}

	result := GenerateResponse{FinishReason: strings.ToUpper(resp.Status)}
	if resp.Usage != nil {
		result.InputTokens = resp.Usage.InputTokens
		result.OutputTokens = resp.Usage.OutputTokens
	}

	if resp.IncompleteDetails != nil && resp.IncompleteDetails.Reason == "max_output_tokens" {
		result.FinishReason = "MAX_TOKENS"
	}

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, content := range item.Content {
				if content.Text != "" {
					result.Text += content.Text
				}
			}
		case "function_call":
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(item.Arguments), &args); err != nil || args == nil {
				args = make(map[string]interface{})
			}
			fc := &FunctionCall{
				ID:         item.CallID,
				Name:       item.Name,
				Args:       args,
				ResponseID: resp.ID,
			}
			result.FunctionCalls = append(result.FunctionCalls, fc)
			result.ModelParts = append(result.ModelParts, Part{FunctionCall: fc})
		}
	}

	return result, nil
}
