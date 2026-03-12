package llm

import "context"

// ToolDeclaration represents a function tool that the LLM can call
type ToolDeclaration struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
}

// GenerateRequest represents a prompt request across any LLM provider
type GenerateRequest struct {
	SystemPrompt  string
	Messages      []Message
	Tools         []ToolDeclaration
	CachedContent string // Used by Gemini
	Temperature   float32
	ResponseJSON  bool // If true, enforce JSON object output
}

// Message represents a conversational turn
type Message struct {
	Role  string // "user", "model", "function"
	Parts []Part
}

// Part represents a component of a message (text or a tool interaction)
type Part struct {
	Text         string
	FunctionCall *FunctionCall
	FunctionResp *FunctionResponse
	IsThought    bool   // Gemini: marks this part as model thinking
	ThoughtSig   string // Gemini: base64-encoded thought signature for this part
}

// FunctionCall represents the LLM deciding to call a tool
type FunctionCall struct {
	ID         string
	Name       string
	Args       map[string]interface{}
	Thought    string // Used by Gemini for thought_signature
	ResponseID string // Used by OpenAI Responses API tool continuation
}

// FunctionResponse represents the result of a tool execution sent back to the LLM
type FunctionResponse struct {
	ID      string
	Name    string
	Content string
}

// GenerateResponse represents the standardized output from any LLM provider
type GenerateResponse struct {
	Text          string
	FunctionCalls []*FunctionCall
	ModelParts    []Part // Full set of model response parts (preserves thought parts for Gemini)
	InputTokens   int
	OutputTokens  int
	FinishReason  string
}

// LLMProvider defines the interface that all underlying AI models must implement
type LLMProvider interface {
	// GenerateContent sends the messages to the LLM and returns the response
	GenerateContent(ctx context.Context, req GenerateRequest) (GenerateResponse, error)

	// CreateCache allows the LLM to cache the static context if supported.
	// Returns a cache ID (or empty string if not supported/needed).
	CreateCache(ctx context.Context, systemPrompt, staticContext string, tools []ToolDeclaration) (string, error)

	// DeleteCache cleans up the cache if applicable.
	DeleteCache(ctx context.Context, cacheID string) error
}
