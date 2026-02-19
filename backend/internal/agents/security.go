package agents

// SecurityAgent builds the config for the Security specialist.
// It receives import graph context, auth function signatures, and security-focused rules.
func SecurityAgent(base AgentConfig) AgentConfig {
	base.Type = AgentSecurity
	base.SystemPrompt = SecuritySystemPrompt
	return base
}
