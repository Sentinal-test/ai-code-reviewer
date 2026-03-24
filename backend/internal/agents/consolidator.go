package agents

import (
	"code-review/backend/internal/models"
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const DefaultMaxComments = 15

// DeterministicConsolidate merges results from all specialist agents into a single ReviewResult.
// It deduplicates by (file, line, message hash), keeps higher severity on conflicts,
// and caps total comments at maxComments.
// This is the deterministic fallback used when LLM-based consolidation fails.
func DeterministicConsolidate(results []AgentResult, maxComments int) *models.ReviewResult {
	if maxComments <= 0 {
		maxComments = DefaultMaxComments
	}

	// Collect all comments with their source agent
	type taggedComment struct {
		Comment models.ReviewComment
		Agent   AgentType
	}

	var all []taggedComment

	for _, r := range results {
		if r.Error != nil {
			continue
		}
		for _, c := range r.Comments {
			all = append(all, taggedComment{Comment: c, Agent: r.Agent})
		}
	}

	// Deduplicate by (file, line, message hash)
	type dedupKey struct {
		File    string
		Line    int
		MsgHash string
	}

	seen := make(map[dedupKey]int) // key → index in deduped slice
	var deduped []models.ReviewComment

	for _, tc := range all {
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Comment.Message)))[:12]
		key := dedupKey{
			File:    tc.Comment.File,
			Line:    tc.Comment.Line,
			MsgHash: hash,
		}

		if idx, exists := seen[key]; exists {
			// Keep the higher severity
			if severityRank(tc.Comment.Severity) > severityRank(deduped[idx].Severity) {
				deduped[idx] = tc.Comment
			}
		} else {
			seen[key] = len(deduped)
			deduped = append(deduped, tc.Comment)
		}
	}

	// 2nd Pass: Deduplicate by (file, msgHash) across different lines
	// For exactly same message in same file, merge into one comment and list lines
	type msgKey struct {
		File    string
		MsgHash string
	}
	msgSeen := make(map[msgKey][]int) // key -> list of line numbers
	msgFinal := make(map[msgKey]models.ReviewComment)

	for _, c := range deduped {
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(c.Message)))[:12]
		key := msgKey{File: c.File, MsgHash: hash}
		msgSeen[key] = append(msgSeen[key], c.Line)

		if existing, exists := msgFinal[key]; !exists || c.Line < existing.Line {
			msgFinal[key] = c
		}
	}

	var final []models.ReviewComment
	for key, comment := range msgFinal {
		lines := msgSeen[key]
		if len(lines) > 1 {
			sort.Ints(lines)
			lineStrs := make([]string, len(lines))
			for i, l := range lines {
				lineStrs[i] = strconv.Itoa(l)
			}
			comment.Message = fmt.Sprintf("Lines %s: %s", strings.Join(lineStrs, ", "), comment.Message)
		}
		final = append(final, comment)
	}

	// Sort: critical → warning → info, then by file, then by line
	sort.Slice(final, func(i, j int) bool {
		ri, rj := severityRank(final[i].Severity), severityRank(final[j].Severity)
		if ri != rj {
			return ri > rj // higher rank first
		}
		if final[i].File != final[j].File {
			return final[i].File < final[j].File
		}
		return final[i].Line < final[j].Line
	})

	// Cap at maxComments
	if len(final) > maxComments {
		final = final[:maxComments]
	}

	// Build combined summary from actual final comments
	criticalCount, warningCount, infoCount := 0, 0, 0
	for _, c := range final {
		switch c.Severity {
		case "critical":
			criticalCount++
		case "warning":
			warningCount++
		case "info":
			infoCount++
		}
	}

	var combinedSummary string
	if len(final) == 0 {
		combinedSummary = "No issues detected"
	} else {
		var parts []string
		if criticalCount > 0 {
			parts = append(parts, fmt.Sprintf("%d critical", criticalCount))
		}
		if warningCount > 0 {
			parts = append(parts, fmt.Sprintf("%d warning", warningCount))
		}
		if infoCount > 0 {
			parts = append(parts, fmt.Sprintf("%d info", infoCount))
		}
		combinedSummary = fmt.Sprintf("Found %s issue(s)", strings.Join(parts, ", "))
	}

	// Merge Resolutions
	var allResolutions []models.Resolution
	for _, r := range results {
		if r.Error == nil {
			allResolutions = append(allResolutions, r.Resolutions...)
		}
	}

	return &models.ReviewResult{
		Summary:     combinedSummary,
		Comments:    final,
		Resolutions: models.MergeResolutions(allResolutions),
	}
}

// severityRank returns a numeric rank for sorting (higher = more severe).
func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}
