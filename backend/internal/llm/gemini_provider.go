package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"
)

type GeminiProvider struct {
	client *genai.Client
	model  string
}

func NewGeminiProvider(apiKey string, model string) (*GeminiProvider, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize gemini client: %w", err)
	}

	if model == "" {
		model = geminiModel // fallback to constant
	}

	return &GeminiProvider{client: client, model: model}, nil
}

func (p *GeminiProvider) CreateCache(ctx context.Context, systemPrompt, staticContext string, tools []ToolDeclaration) (string, error) {
	// genai SDK supports caching.
	// Minimum tokens required is ~32k.
	var declarations []*genai.FunctionDeclaration
	for _, t := range tools {
		// Convert parameters map to genai.Schema via JSON
		bytes, _ := json.Marshal(t.Parameters)
		var schema genai.Schema
		json.Unmarshal(bytes, &schema)

		declarations = append(declarations, &genai.FunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  &schema,
		})
	}

	systemContent := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: systemPrompt}},
	}
	userContent := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: staticContext}},
	}
	modelContent := &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{{Text: "Understood. I have stored this context and await your instructions."}},
	}

	config := &genai.CreateCachedContentConfig{
		SystemInstruction: systemContent,
		Contents:          []*genai.Content{userContent, modelContent},
		TTL:               15 * time.Minute,
	}

	if len(tools) > 0 {
		config.Tools = []*genai.Tool{{
			FunctionDeclarations: p.toGenaiFunctionDeclarations(tools),
		}}
	}

	resp, err := p.client.Caches.Create(ctx, p.model, config)
	if err != nil {
		return "", fmt.Errorf("gemini cache creation failed: %w", err)
	}

	return resp.Name, nil
}

func (p *GeminiProvider) DeleteCache(ctx context.Context, cacheName string) error {
	_, err := p.client.Caches.Delete(ctx, cacheName, &genai.DeleteCachedContentConfig{})
	return err
}

func (p *GeminiProvider) toGenaiFunctionDeclarations(tools []ToolDeclaration) []*genai.FunctionDeclaration {
	var declarations []*genai.FunctionDeclaration
	for _, t := range tools {
		bytes, _ := json.Marshal(t.Parameters)
		var schema genai.Schema
		json.Unmarshal(bytes, &schema)
		declarations = append(declarations, &genai.FunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  &schema,
		})
	}
	return declarations
}

func (p *GeminiProvider) GenerateContent(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	config := &genai.GenerateContentConfig{
		Temperature: genai.Ptr(float32(req.Temperature)),
	}

	if req.ResponseJSON {
		config.ResponseMIMEType = "application/json"
		// We could add ResponseSchema, but our prompt enforces it stringently.
	}

	if req.CachedContent != "" {
		config.CachedContent = req.CachedContent
	}

	if req.SystemPrompt != "" {
		config.SystemInstruction = &genai.Content{
			Role:  "system",
			Parts: []*genai.Part{{Text: req.SystemPrompt}},
		}
	}

	if len(req.Tools) > 0 {
		config.Tools = []*genai.Tool{{FunctionDeclarations: p.toGenaiFunctionDeclarations(req.Tools)}}
		// genai.ToolConfig requires setting mode
		config.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: "AUTO",
			},
		}
	}

	var contents []*genai.Content
	for _, m := range req.Messages {
		var parts []*genai.Part
		for _, p := range m.Parts {
			part := &genai.Part{}
			if p.Text != "" {
				part.Text = p.Text
			} else if p.FunctionCall != nil {
				argsBytes, _ := json.Marshal(p.FunctionCall.Args)
				var argsMap map[string]any
				json.Unmarshal(argsBytes, &argsMap)
				part.FunctionCall = &genai.FunctionCall{
					Name: p.FunctionCall.Name,
					Args: argsMap,
				}
			} else if p.FunctionResp != nil {
				argsMap := map[string]any{
					"content": p.FunctionResp.Content,
				}
				part.FunctionResponse = &genai.FunctionResponse{
					Name:     p.FunctionResp.Name,
					Response: argsMap,
				}
			}
			parts = append(parts, part)
		}

		role := m.Role

		contents = append(contents, &genai.Content{
			Role:  role,
			Parts: parts,
		})
	}

	resp, err := p.client.Models.GenerateContent(ctx, p.model, contents, config)
	if err != nil {
		return GenerateResponse{}, err
	}

	if len(resp.Candidates) == 0 {
		return GenerateResponse{FinishReason: "EMPTY"}, nil
	}

	candidate := resp.Candidates[0]
	// Parse response
	var finishReason string
	if candidate.FinishReason != "" {
		finishReason = string(candidate.FinishReason)
	}

	var result GenerateResponse
	result.FinishReason = finishReason

	if resp.UsageMetadata != nil {
		result.InputTokens = int(resp.UsageMetadata.PromptTokenCount)
		result.OutputTokens = int(resp.UsageMetadata.CandidatesTokenCount)
	}

	if candidate.Content != nil {
		var textParts []string
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			} else if part.FunctionCall != nil {
				result.FunctionCall = &FunctionCall{
					Name: part.FunctionCall.Name,
					Args: part.FunctionCall.Args,
				}
			}
		}
		result.Text = strings.Join(textParts, "\n")
	}

	return result, nil
}
