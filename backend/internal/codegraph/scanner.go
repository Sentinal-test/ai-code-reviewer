package codegraph

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Scanner locates potential files containing symbol definitions
type Scanner struct {
	RepoPath string
}

func NewScanner(repoPath string) *Scanner {
	return &Scanner{RepoPath: repoPath}
}

// FindCandidates searches for files that might contain the definition of a symbol
// It uses git grep for speed, or a fallback if needed (though we assume git environment)
func (s *Scanner) FindCandidates(ctx context.Context, symbol string, langExts []string) ([]string, error) {
	// Construct grep pattern: look for "func symbol", "class symbol", "type symbol" etc.
	// For simplicity and robustness, we'll just search for the symbol string first,
	// relying on the subsequent AST parse to confirm if it's a definition.
	// To reduce noise, we can search for the symbol with word boundaries.

	// Prepare arguments for git grep
	// -l: list filenames only
	// -F: fixed string search (faster, safer)
	// -w: word boundary
	args := []string{"grep", "-l", "-F", "-w", symbol}

	// Add extension filters if provided
	if len(langExts) > 0 {
		// git grep doesn't natively support --include params easily in all versions in the same way,
		// but we can just filter the output or let it search everything (it's fast).
		// Better: use -- globs at the end
		args = append(args, "--")
		for _, ext := range langExts {
			args = append(args, fmt.Sprintf("*%s", ext))
		}
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = s.RepoPath

	var out bytes.Buffer
	cmd.Stdout = &out
	// Ignore errors from grep (exit code 1 means no matches)
	_ = cmd.Run()

	output := out.String()
	if output == "" {
		return []string{}, nil
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	return lines, nil
}
