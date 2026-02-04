package action

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GetDiff returns the git diff between two references.
func GetDiff(base, head string) (string, error) {
	// git diff base...head
	cmd := exec.Command("git", "diff", fmt.Sprintf("%s...%s", base, head))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %s: %w", string(out), err)
	}
	return string(out), nil
}

// GetChangedFiles returns a list of files that have changed between base and head.
func GetChangedFiles(base, head string) ([]string, error) {
	// git diff --name-only base...head
	cmd := exec.Command("git", "diff", "--name-only", fmt.Sprintf("%s...%s", base, head))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only failed: %s: %w", string(out), err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			files = append(files, strings.TrimSpace(line))
		}
	}
	return files, nil
}

// GetFileContent reads a file from the local filesystem.
// In Action mode, the repo is checked out, so we just read the file.
func GetFileContent(path string) (string, error) {
	// Using os.ReadFile avoids command injection vulnerabilities
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return string(content), nil
}

// GetRepoStructure generates a simple tree-like structure of the repo to give context to the LLM.
// Using `git ls-files` is often cleaner than walking directories manually and respecting gitignore.
func GetRepoStructure() (string, error) {
	cmd := exec.Command("git", "ls-files")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git ls-files failed: %s: %w", string(out), err)
	}
	return string(out), nil
}
