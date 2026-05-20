package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
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

	if envModel := os.Getenv("GEMINI_MODEL"); envModel != "" {
		model = envModel
	} else if model == "" {
		model = geminiModel // fallback to constant
	}

	return &GeminiProvider{client: client, model: model}, nil
}

func (p *GeminiProvider) CreateCache(ctx context.Context, systemPrompt, staticContext string, tools []ToolDeclaration) (string, error) {
	// genai SDK supports caching.
	// Minimum tokens required is ~32k.
	systemContent := &genai.Content{
		Role:  "system",
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

func (p *GeminiProvider) buildGenerateConfig(req GenerateRequest) *genai.GenerateContentConfig {
	config := &genai.GenerateContentConfig{
		Temperature: genai.Ptr(float32(req.Temperature)),
	}

	if req.ResponseJSON {
		config.ResponseMIMEType = "application/json"
		if req.ResponseSchema != nil {
			bytes, _ := json.Marshal(req.ResponseSchema)
			var schema genai.Schema
			json.Unmarshal(bytes, &schema)
			config.ResponseSchema = &schema
		}
	}

	if req.CachedContent != "" {
		// Gemini rejects GenerateContent requests that combine CachedContent with
		// system instructions or tool declarations. Those must already be part of
		// the cached content created via CreateCache.
		config.CachedContent = req.CachedContent
		return config
	}

	if req.SystemPrompt != "" {
		config.SystemInstruction = &genai.Content{
			Role:  "system",
			Parts: []*genai.Part{{Text: req.SystemPrompt}},
		}
	}

	if len(req.Tools) > 0 {
		config.Tools = []*genai.Tool{{FunctionDeclarations: p.toGenaiFunctionDeclarations(req.Tools)}}
		config.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: "AUTO",
			},
		}
	}

	return config
}

func (p *GeminiProvider) GenerateContent(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	config := p.buildGenerateConfig(req)

	var contents []*genai.Content
	for _, m := range req.Messages {
		var parts []*genai.Part
		for _, p := range m.Parts {
			part := &genai.Part{}

			// Restore thought metadata on ANY part type
			if p.IsThought {
				part.Thought = true
			}
			if p.ThoughtSig != "" {
				decodedSig, _ := base64.StdEncoding.DecodeString(p.ThoughtSig)
				part.ThoughtSignature = decodedSig
			}

			if p.FunctionCall != nil {
				argsBytes, _ := json.Marshal(p.FunctionCall.Args)
				var argsMap map[string]any
				json.Unmarshal(argsBytes, &argsMap)
				part.FunctionCall = &genai.FunctionCall{
					ID:   p.FunctionCall.ID,
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
			} else if p.Text != "" {
				part.Text = p.Text
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
		result.CachedTokens = int(resp.UsageMetadata.CachedContentTokenCount)
		result.ToolUsePromptTokens = int(resp.UsageMetadata.ToolUsePromptTokenCount)
		result.ThoughtsTokens = int(resp.UsageMetadata.ThoughtsTokenCount)
		result.TotalTokens = int(resp.UsageMetadata.TotalTokenCount)
	}

	// Capture ALL parts from the response — including thought parts.
	// Gemini 2.0+ requires these to be replayed verbatim in subsequent turns.
	if candidate.Content != nil {
		var textParts []string
		for _, part := range candidate.Content.Parts {
			// Base64 encode the thought signature for safe string transport
			encodedSig := ""
			if len(part.ThoughtSignature) > 0 {
				encodedSig = base64.StdEncoding.EncodeToString(part.ThoughtSignature)
			}

			if part.FunctionCall != nil {
				fc := &FunctionCall{
					ID:   part.FunctionCall.ID,
					Name: part.FunctionCall.Name,
					Args: part.FunctionCall.Args,
				}
				result.FunctionCalls = append(result.FunctionCalls, fc)
				result.ModelParts = append(result.ModelParts, Part{
					FunctionCall: fc,
					IsThought:    part.Thought,
					ThoughtSig:   encodedSig,
				})
			} else if part.Text != "" {
				if !part.Thought {
					textParts = append(textParts, part.Text)
				}
				// Always capture the part (thought or not) for history replay
				result.ModelParts = append(result.ModelParts, Part{
					Text:       part.Text,
					IsThought:  part.Thought,
					ThoughtSig: encodedSig,
				})
			}
		}
		result.Text = strings.Join(textParts, "\n")
	}

	return result, nil
}

func (p *GeminiProvider) GetName() string {
	return "Gemini (" + p.model + ")"
}
