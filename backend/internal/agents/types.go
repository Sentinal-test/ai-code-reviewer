package agents

import "code-review/backend/internal/models"

// AgentType identifies which specialist agent produced a result.
type AgentType string

const (
	AgentCorrectness AgentType = "correctness" // 🐛 Bugs + Performance
	AgentSecurity    AgentType = "security"    // 🔐 Security
	AgentStructure   AgentType = "structure"   // 🏗️ Architecture + Lint
)

// AgentConfig is the input bundle passed to each specialist agent.
type AgentConfig struct {
	// Identity
	Type         AgentType
	SystemPrompt string

	// Code context
	ChangedFiles map[string]string // path → full file content
	Diff         string            // unified diff for the chunk
	Dependencies map[string]string // scoped code graph dependencies

	// Metadata
	RepoStructure string           // repo tree (mainly for Structure agent)
	PRContext     models.PRContext // PR title, body, commit messages
	CrossRefs     []string         // files in other chunks this chunk depends on

	// Chunk info
	ChunkIndex int
	ChunkTotal int

	// LLM
	RepoPath string // for tool calls (file reads, git grep)

	// Multi-repo
	MatchSummary string

	// Developer rules
	DeveloperRules *models.DeveloperRules

	// Caching
	CacheID string

	// History (PR Memory)
	PreviousFindings []models.PreviousFinding
}

// AgentResult is the output from a single specialist agent.
type AgentResult struct {
	Agent        AgentType              `json:"agent"`
	Comments     []models.ReviewComment `json:"comments"`
	Resolutions  []models.Resolution    `json:"resolutions"`
	Summary      string                 `json:"summary"`
	ToolCalls    int                    `json:"tool_calls"` // how many tool calls were made
	InputTokens  int                    `json:"input_tokens"`
	OutputTokens int                    `json:"output_tokens"`
	Error        error                  `json:"-"` // internal error (not serialized)
}

// ToolDeclaration describes a tool available to agents via Gemini function calling.
type ToolDeclaration struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ToolCallRequest is what Gemini returns when it wants to call a tool.
type ToolCallRequest struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// ToolCallResponse is the result of executing a tool locally.
type ToolCallResponse struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}


type AgentSummary struct {