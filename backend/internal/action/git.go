package action

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// GetDiff returns the git diff between two references.
// It first tries a three-dot diff (merge-base aware), which is ideal for PRs/MRs.
// If that fails (e.g. no merge base in CI), it falls back to a two-dot diff.
func GetDiff(base, head string) (string, error) {
	// Try three-dot diff first (merge-base aware, best for PRs/MRs)
	cmd := exec.Command("git", "diff", fmt.Sprintf("%s...%s", base, head))
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := string(out)
		// Fall back to two-dot diff if no merge base is found
		if strings.Contains(outStr, "no merge base") {
			fmt.Println("⚠️  No merge base found, falling back to two-dot diff...")
			cmd = exec.Command("git", "diff", base, head)
			out, err = cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("git diff (fallback) failed: %s: %w", string(out), err)
			}
			return string(out), nil
		}
		return "", fmt.Errorf("git diff failed: %s: %w", outStr, err)
	}
	return string(out), nil
}

// GetChangedFiles returns a list of files that have changed between base and head.
// Like GetDiff, it falls back to two-dot diff if no merge base is found.
func GetChangedFiles(base, head string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", fmt.Sprintf("%s...%s", base, head))
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := string(out)
		if strings.Contains(outStr, "no merge base") {
			fmt.Println("⚠️  No merge base found for --name-only, falling back to two-dot diff...")
			cmd = exec.Command("git", "diff", "--name-only", base, head)
			out, err = cmd.CombinedOutput()
			if err != nil {
				return nil, fmt.Errorf("git diff --name-only (fallback) failed: %s: %w", string(out), err)
			}
		} else {
			return nil, fmt.Errorf("git diff --name-only failed: %s: %w", outStr, err)
		}
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

// GetRepoStructure returns a compact repository layout summary for the Structure
// agent. Raw `git ls-files` output can add thousands of prompt lines without
// improving architecture review quality.
func GetRepoStructure() (string, error) {
	cmd := exec.Command("git", "ls-files")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git ls-files failed: %s: %w", string(out), err)
	}
	return summarizeRepoFiles(string(out)), nil
}

func summarizeRepoFiles(lsOutput string) string {
	const (
		maxRootFiles     = 25
		maxDirectories   = 300
		maxSamplesPerDir = 5
	)

	lines := strings.Split(lsOutput, "\n")
	rootFiles := make([]string, 0)
	dirCounts := make(map[string]int)
	dirSamples := make(map[string][]string)

	for _, line := range lines {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}

		dir := filepath.ToSlash(filepath.Dir(path))
		base := filepath.Base(path)
		if dir == "." {
			rootFiles = append(rootFiles, base)
			continue
		}

		dirCounts[dir]++
		if len(dirSamples[dir]) < maxSamplesPerDir {
			dirSamples[dir] = append(dirSamples[dir], base)
		}
	}

	sort.Strings(rootFiles)

	dirs := make([]string, 0, len(dirCounts))
	for dir := range dirCounts {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	var b strings.Builder
	b.WriteString("REPOSITORY LAYOUT SUMMARY\n")

	if len(rootFiles) > 0 {
		b.WriteString("\nROOT FILES:\n")
		limit := len(rootFiles)
		if limit > maxRootFiles {
			limit = maxRootFiles
		}
		for _, file := range rootFiles[:limit] {
			b.WriteString("- ")
			b.WriteString(file)
			b.WriteString("\n")
		}
		if len(rootFiles) > limit {
			b.WriteString(fmt.Sprintf("- ... (%d more root files)\n", len(rootFiles)-limit))
		}
	}

	b.WriteString("\nDIRECTORIES:\n")
	limit := len(dirs)
	if limit > maxDirectories {
		limit = maxDirectories
	}
	for _, dir := range dirs[:limit] {
		samples := append([]string(nil), dirSamples[dir]...)
		sort.Strings(samples)

		b.WriteString("- ")
		b.WriteString(dir)
		b.WriteString(fmt.Sprintf(" (%d files)", dirCounts[dir]))
		if len(samples) > 0 {
			b.WriteString(": ")
			b.WriteString(strings.Join(samples, ", "))
			if dirCounts[dir] > len(samples) {
				b.WriteString(", ...")
			}
		}
		b.WriteString("\n")
	}
	if len(dirs) > limit {
		b.WriteString(fmt.Sprintf("- ... (%d more directories)\n", len(dirs)-limit))
	}

	return b.String()
}
