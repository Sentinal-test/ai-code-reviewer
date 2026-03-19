package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	specOnce  sync.Once
	specRoot  string
	specError error
)

func BuildSpecialistSystemPrompt(agentDir, basePrompt, multiRepoBlock string) string {
	return buildPromptWithSpecs(agentDir, basePrompt+fmt.Sprintf(sharedRules, multiRepoBlock), false)
}

func BuildConsolidatorSystemPrompt(maxComments int) string {
	return buildPromptWithSpecs("consolidator", fmt.Sprintf(ConsolidatorSystemPrompt, maxComments), true)
}

func buildPromptWithSpecs(agentDir, basePrompt string, includeSystemSpec bool) string {
	specBlock := loadSpecBlock(agentDir, includeSystemSpec)
	if specBlock == "" {
		return basePrompt
	}
	return specBlock + "\n\n" + basePrompt
}

func loadSpecBlock(agentDir string, includeSystemSpec bool) string {
	root, err := repoRoot()
	if err != nil || root == "" {
		return ""
	}

	agentSpec := readSpec(filepath.Join(root, "agents", agentDir, "Agent.md"))
	instructions := readSpec(filepath.Join(root, "agents", agentDir, "INSTRUCTIONS.md"))

	var parts []string
	if includeSystemSpec {
		systemSpec := readSpec(filepath.Join(root, "AGENTS.md"))
		if systemSpec != "" {
			parts = append(parts, "SYSTEM ARCHITECTURE REFERENCE\n"+systemSpec)
		}
	}
	if agentSpec != "" {
		parts = append(parts, "AGENT IDENTITY REFERENCE\n"+agentSpec)
	}
	if instructions != "" {
		parts = append(parts, "AGENT WORKFLOW REFERENCE\n"+instructions)
	}

	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func repoRoot() (string, error) {
	specOnce.Do(func() {
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			specError = fmt.Errorf("runtime.Caller failed")
			return
		}
		specRoot = filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	})
	return specRoot, specError
}

func readSpec(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
