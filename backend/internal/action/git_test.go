package action

import (
	"strings"
	"testing"
)

func TestSummarizeRepoFiles(t *testing.T) {
	input := strings.Join([]string{
		"README.md",
		"backend/cmd/cli/main.go",
		"backend/cmd/cli/main_test.go",
		"backend/internal/llm/agent_loop.go",
		"backend/internal/llm/consolidator.go",
		"backend/internal/llm/gemini_provider.go",
	}, "\n")

	got := summarizeRepoFiles(input)

	for _, want := range []string{
		"REPOSITORY LAYOUT SUMMARY",
		"ROOT FILES:",
		"- README.md",
		"- backend/cmd/cli (2 files): main.go, main_test.go",
		"- backend/internal/llm (3 files): agent_loop.go, consolidator.go, gemini_provider.go",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q\n%s", want, got)
		}
	}
}
