package utils

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// hunkHeaderRegex matches unified diff hunk headers like @@ -10,5 +12,8 @@
var hunkHeaderRegex = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// ExtractValidDiffLines parses a unified diff and returns a map of
// file path → sorted slice of valid line numbers on the new-file (RIGHT) side.
// Only lines that appear as added (+) or context (unchanged) within hunks are valid
// for inline comments.
func ExtractValidDiffLines(diff string) map[string][]int {
	result := make(map[string][]int)
	lines := strings.Split(diff, "\n")

	var currentFile string
	var lineNum int

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			// Extract file path from "diff --git a/path b/path"
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				currentFile = strings.TrimPrefix(parts[3], "b/")
				currentFile = strings.Trim(currentFile, "\"")
			}
			continue
		}

		if strings.HasPrefix(line, "@@") && currentFile != "" {
			matches := hunkHeaderRegex.FindStringSubmatch(line)
			if len(matches) >= 2 {
				lineNum, _ = strconv.Atoi(matches[1])
			}
			continue
		}

		if currentFile == "" || lineNum == 0 {
			continue
		}

		if strings.HasPrefix(line, "+") {
			// Added line — valid for inline comment
			result[currentFile] = append(result[currentFile], lineNum)
			lineNum++
		} else if strings.HasPrefix(line, "-") {
			// Deleted line — no new-file line number, skip
			continue
		} else if strings.HasPrefix(line, "\\") {
			// "No newline at end of file" — skip
			continue
		} else if strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			// Diff metadata — skip
			continue
		} else {
			// Context (unchanged) line — valid for inline comment
			result[currentFile] = append(result[currentFile], lineNum)
			lineNum++
		}
	}

	// Sort each file's lines for binary search
	for f := range result {
		sort.Ints(result[f])
	}
	return result
}

// SnapToValidLine finds the nearest valid diff line for a given line number.
// Returns 0 if no valid lines exist for the file.
func SnapToValidLine(line int, validLines []int) int {
	if len(validLines) == 0 {
		return 0
	}

	// Check if exact match exists
	idx := sort.SearchInts(validLines, line)
	if idx < len(validLines) && validLines[idx] == line {
		return line
	}

	// Find nearest
	best := validLines[0]
	bestDist := int(math.Abs(float64(line - best)))

	if idx < len(validLines) {
		d := int(math.Abs(float64(line - validLines[idx])))
		if d < bestDist {
			best = validLines[idx]
			bestDist = d
		}
	}
	if idx > 0 {
		d := int(math.Abs(float64(line - validLines[idx-1])))
		if d < bestDist {
			best = validLines[idx-1]
		}
	}

	return best
}
