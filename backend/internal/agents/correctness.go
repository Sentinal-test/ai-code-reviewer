package agents

// CorrectnessAgent builds the config for the Bugs + Performance specialist.
// It receives data flow edges, function call chains, and type definitions from the code graph.
func CorrectnessAgent(base AgentConfig) AgentConfig {
	base.Type = AgentCorrectness
	base.SystemPrompt = CorrectnessSystemPrompt
	return base
}
