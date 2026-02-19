package agents

// StructureAgent builds the config for the Architecture + Lint specialist.
// It receives full repo tree structure, dependency direction graph, and interface definitions.
func StructureAgent(base AgentConfig) AgentConfig {
	base.Type = AgentStructure
	base.SystemPrompt = StructureSystemPrompt
	return base
}
