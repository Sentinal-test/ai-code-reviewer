package action

import (
	"fmt"
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
	// We could use os.ReadFile, but using git show head:path ensures we see what's in the commit
	// However, for simplicity and since we checked out the code, os.ReadFile is fine.
	// Actually, strictly following "checked out locally", using `cat` or `git show` is safer if the checkout is partial?
	// `actions/checkout` usually checks out the HEAD commit.
	// But let's stick to Local Filesystem as per requirements: "Read files directly from the local filesystem"

	// Implementation note: The user requirement says "Read files directly from the local filesystem"
	// So we will use a simple file read, assuming the runner worked correctly.

	cmd := exec.Command("cat", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %s: %w", path, string(out), err)
	}
	return string(out), nil
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
