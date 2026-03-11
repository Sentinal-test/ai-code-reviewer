package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sashabaranov/go-openai"
)

const (
	// Default OpenAI model for code reviews
	openAIModel = openai.GPT4o
)

type OpenAIProvider struct {
	client *openai.Client
	model  string
}

func NewOpenAIProvider(apiKey string, model string) *OpenAIProvider {
	client := openai.NewClient(apiKey)
	if model == "" {
		model = openAIModel // Note: using standard models
	}
	return &OpenAIProvider{client: client, model: model}
}

func (p *OpenAIProvider) CreateCache(ctx context.Context, systemPrompt, staticContext string, tools []ToolDeclaration) (string, error) {
	// OpenAI handles prompt caching automatically based on exact prefix matching.
	// We don't need to manually create a cache resource.
	// We return an empty string, meaning caching is a no-op.
	return "", nil
}

func (p *OpenAIProvider) DeleteCache(ctx context.Context, cacheID string) error {
	// No-op for OpenAI
	return nil
}

func (p *OpenAIProvider) GenerateContent(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	var messages []openai.ChatCompletionMessage

	// Add System message, incorporating cached content/static context if it was "cached" locally
	if req.SystemPrompt != "" {
		msgs := []string{req.SystemPrompt}
		if req.CachedContent != "" {
			msgs = append(msgs, "Project Context:", req.CachedContent)
		}

		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: strings.Join(msgs, "\n\n"),
		})
	}

	for _, m := range req.Messages {
		role := m.Role
		if role == "model" {
			role = openai.ChatMessageRoleAssistant
		} else if role == "function" {
			role = openai.ChatMessageRoleTool
		}

		// Handle pure text messages
		if len(m.Parts) == 1 && m.Parts[0].Text != "" && role != openai.ChatMessageRoleTool {
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    role,
				Content: m.Parts[0].Text,
			})
			continue
		}

		// Handle complex parts (function calls & responses)
		msg := openai.ChatCompletionMessage{
			Role: role,
		}

		var toolCalls []openai.ToolCall
		for _, part := range m.Parts {
			if part.Text != "" {
				msg.Content += part.Text + "\n"
			} else if part.FunctionCall != nil {
				// We append a tool call
				bytes, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, openai.ToolCall{
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      part.FunctionCall.Name,
						Arguments: string(bytes),
					},
					ID: part.FunctionCall.ID,
				})
			} else if part.FunctionResp != nil {
				// OpenAI maps function responses directly as a separate message format.
				messages = append(messages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    part.FunctionResp.Content,
					Name:       part.FunctionResp.Name,
					ToolCallID: part.FunctionResp.ID,
				})
			}
		}

		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}

		// If it's a tool response, it was already appended
		if role != openai.ChatMessageRoleTool {
			messages = append(messages, msg)
		}
	}

	var tools []openai.Tool
	for _, t := range req.Tools {
		tools = append(tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	apiReq := openai.ChatCompletionRequest{
		Model:       p.model,
		Messages:    messages,
		Temperature: req.Temperature,
	}

	if req.ResponseJSON {
		apiReq.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		}
	}

	if len(tools) > 0 {
		apiReq.Tools = tools
	}

	resp, err := p.client.CreateChatCompletion(ctx, apiReq)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("openai error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return GenerateResponse{FinishReason: "EMPTY"}, nil
	}

	choice := resp.Choices[0]
	result := GenerateResponse{
		FinishReason: string(choice.FinishReason),
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}

	if choice.Message.Content != "" {
		result.Text = choice.Message.Content
	}

	if len(choice.Message.ToolCalls) > 0 {
		tc := choice.Message.ToolCalls[0]
		var args map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		result.FunctionCall = &FunctionCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: args,
		}
	}

	return result, nil
}
