package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liushuangls/go-anthropic/v2"
)

type ClaudeProvider struct {
	client *anthropic.Client
	model  string
}

func NewClaudeProvider(apiKey string, model string) *ClaudeProvider {
	client := anthropic.NewClient(apiKey)
	if model == "" {
		model = string(anthropic.ModelClaude3Dot5Sonnet20241022)
	}
	return &ClaudeProvider{client: client, model: model}
}

func (p *ClaudeProvider) CreateCache(ctx context.Context, systemPrompt, staticContext string, tools []ToolDeclaration) (string, error) {
	// Anthropic handles caching implicitly via 'cache_control' on headers.
	// Similar to OpenAI, we don't need a dedicated caching API call here.
	return "", nil
}

func (p *ClaudeProvider) DeleteCache(ctx context.Context, cacheID string) error {
	return nil
}

func (p *ClaudeProvider) GenerateContent(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	var messages []anthropic.Message

	for _, m := range req.Messages {
		role := m.Role
		if role == "model" {
			role = string(anthropic.RoleAssistant)
		} else if role == "user" {
			role = string(anthropic.RoleUser) // Anthropic models treat tool results as user messages
		}

		var blocks []anthropic.MessageContent
		for _, part := range m.Parts {
			if part.Text != "" {
				blocks = append(blocks, anthropic.NewTextMessageContent(part.Text))
			} else if part.FunctionCall != nil {
				argsBytes, _ := json.Marshal(part.FunctionCall.Args)
				blocks = append(blocks, anthropic.NewToolUseMessageContent("call_"+part.FunctionCall.Name, part.FunctionCall.Name, argsBytes))
			} else if part.FunctionResp != nil {
				// Tool Result
				respBytes, _ := json.Marshal(part.FunctionResp.Content)
				resContent := anthropic.NewToolResultMessageContent(
					"call_"+part.FunctionResp.Name, // ID must match what we got
					string(respBytes),
					false,
				)
				blocks = append(blocks, resContent)
			}
		}

		messages = append(messages, anthropic.Message{
			Role:    anthropic.ChatRole(role),
			Content: blocks,
		})
	}

	var tools []anthropic.ToolDefinition
	for _, t := range req.Tools {
		// Convert the parameters standard map to json.RawMessage
		bytes, _ := json.Marshal(t.Parameters)

		tools = append(tools, anthropic.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: bytes,
		})
	}

	systemStr := req.SystemPrompt
	if req.CachedContent != "" {
		systemStr += "\n\nProject Context:\n" + req.CachedContent
	}

	temp := req.Temperature
	apiReq := anthropic.MessagesRequest{
		Model:       anthropic.Model(p.model),
		Messages:    messages,
		System:      systemStr,
		Temperature: &temp,
		MaxTokens:   4096, // required by Anthropic
	}

	if len(tools) > 0 {
		apiReq.Tools = tools
	}

	resp, err := p.client.CreateMessages(ctx, apiReq)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("claude error: %w", err)
	}

	result := GenerateResponse{
		FinishReason: string(resp.StopReason),
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
	}

	for _, block := range resp.Content {
		if block.Type == anthropic.MessagesContentTypeText {
			result.Text += *block.Text
		} else if block.Type == anthropic.MessagesContentTypeToolUse {
			result.FunctionCall = &FunctionCall{
				Name: block.Name,
			}

			// Try to unmarshal args
			var args map[string]interface{}
			if err := json.Unmarshal(block.Input, &args); err == nil {
				result.FunctionCall.Args = args
			}
		}
	}

	return result, nil
}
