package llm

import (
	"testing"

	"google.golang.org/genai"
)

func TestBuildGenerateConfig_OmitsSystemAndToolsWhenUsingCachedContent(t *testing.T) {
	provider := &GeminiProvider{}
	req := GenerateRequest{
		SystemPrompt:  "system",
		CachedContent: "cache-id",
		Temperature:   0.1,
		Tools: []ToolDeclaration{{
			Name:        "search_codebase",
			Description: "search",
			Parameters:  map[string]interface{}{"type": "object"},
		}},
	}

	config := provider.buildGenerateConfig(req)
	if config.CachedContent != "cache-id" {
		t.Fatalf("expected cached content to be set")
	}
	if config.SystemInstruction != nil {
		t.Fatalf("expected system instruction to be omitted when using cached content")
	}
	if len(config.Tools) != 0 {
		t.Fatalf("expected tools to be omitted when using cached content")
	}
	if config.ToolConfig != nil {
		t.Fatalf("expected tool config to be omitted when using cached content")
	}
}

func TestBuildGenerateConfig_IncludesSystemAndToolsWithoutCachedContent(t *testing.T) {
	provider := &GeminiProvider{}
	req := GenerateRequest{
		SystemPrompt: "system",
		Temperature:  0.1,
		Tools: []ToolDeclaration{{
			Name:        "search_codebase",
			Description: "search",
			Parameters:  map[string]interface{}{"type": "object"},
		}},
	}

	config := provider.buildGenerateConfig(req)
	if config.CachedContent != "" {
		t.Fatalf("did not expect cached content")
	}
	if config.SystemInstruction == nil {
		t.Fatalf("expected system instruction to be included without cached content")
	}
	if len(config.Tools) != 1 {
		t.Fatalf("expected one tool to be included, got %d", len(config.Tools))
	}
	if config.ToolConfig == nil {
		t.Fatalf("expected tool config to be included without cached content")
	}
}

func TestBuildGenerateConfig_SetsThinkingLevelForGemini35Flash(t *testing.T) {
	provider := &GeminiProvider{model: "gemini-3.5-flash"}
	req := GenerateRequest{
		Temperature: 0.1,
	}

	config := provider.buildGenerateConfig(req)
	if config.ThinkingConfig == nil {
		t.Fatalf("expected ThinkingConfig to be set")
	}
	if config.ThinkingConfig.ThinkingLevel != genai.ThinkingLevelHigh {
		t.Fatalf("expected ThinkingLevel to be HIGH, got %v", config.ThinkingConfig.ThinkingLevel)
	}
}
