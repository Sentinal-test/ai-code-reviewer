package action

import (
	"fmt"
	"os/exec"
	"strings"
)

// ensureHistory attempts to fetch enough history for valid diffs if the first attempt failed.
func ensureHistory(base, head string) {
	fmt.Printf("🔄 Attempting to fetch missing history for %s...%s\n", base, head)

	// 1. Try fetching just the missing commits (if remote supports it)
	exec.Command("git", "fetch", "origin", base).Run()
	exec.Command("git", "fetch", "origin", head).Run()

	// 2. If still shallow, try to unshallow (ignoring errors as it fails if already full)
	exec.Command("git", "fetch", "--unshallow").Run()

	// 3. Fallback: Fetch everything
	exec.Command("git", "fetch", "--all").Run()
	exec.Command("git", "fetch", "origin").Run()
}

// GetDiff returns the git diff between two references.
func GetDiff(base, head string) (string, error) {
	// git diff base...head
	cmd := exec.Command("git", "diff", fmt.Sprintf("%s...%s", base, head))
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), nil
	}

	// If failed, try to fetch history and retry
	errStr := string(out)
	if strings.Contains(errStr, "unknown revision") || strings.Contains(errStr, "Invalid symmetric difference") {
		ensureHistory(base, head)

		// Retry
		cmdRetry := exec.Command("git", "diff", fmt.Sprintf("%s...%s", base, head))
		outRetry, errRetry := cmdRetry.CombinedOutput()
		if errRetry == nil {
			fmt.Printf("✅ Recovered from diff error after fetching history.\n")
			return string(outRetry), nil
		}

		// Final Fallback: Direct diff (space separated)
		// This compares the two tips directly, ignoring the merge base.
		fmt.Printf("⚠️ Symmetric diff failed even after fetch. Falling back to direct diff (%s %s).\n", base, head)
		cmdFallback := exec.Command("git", "diff", base, head)
		outFallback, errFallback := cmdFallback.CombinedOutput()
		if errFallback == nil {
			return string(outFallback), nil
		}

		return "", fmt.Errorf("git diff fallback failed: %s: %w (original error: %s)", string(outFallback), errFallback, string(outRetry))
	}

	return "", fmt.Errorf("git diff failed: %s: %w", string(out), err)
}

// GetChangedFiles returns a list of files that have changed between base and head.
func GetChangedFiles(base, head string) ([]string, error) {
	// git diff --name-only base...head
	cmd := exec.Command("git", "diff", "--name-only", fmt.Sprintf("%s...%s", base, head))
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Try to fix history same as GetDiff
		ensureHistory(base, head)

		cmdRetry := exec.Command("git", "diff", "--name-only", fmt.Sprintf("%s...%s", base, head))
		outRetry, errRetry := cmdRetry.CombinedOutput()
		if errRetry != nil {
			// Fallback to direct diff
			cmdFallback := exec.Command("git", "diff", "--name-only", base, head)
			outFallback, errFallback := cmdFallback.CombinedOutput()
			if errFallback != nil {
				return nil, fmt.Errorf("git diff --name-only fallback failed: %s: %w", string(outFallback), errFallback)
			}
			outRetry = outFallback
		}
		out = outRetry
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
