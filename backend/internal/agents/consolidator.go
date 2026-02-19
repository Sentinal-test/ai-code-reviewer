package agents

import (
	"code-review/backend/internal/models"
	"crypto/sha256"
	"fmt"
	"sort"
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
	var summaries []string

	for _, r := range results {
		if r.Error != nil {
			continue
		}
		for _, c := range r.Comments {
			all = append(all, taggedComment{Comment: c, Agent: r.Agent})
		}
		if r.Summary != "" {
			summaries = append(summaries, fmt.Sprintf("[%s] %s", r.Agent, r.Summary))
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

	// Sort: critical → warning → info, then by file, then by line
	sort.Slice(deduped, func(i, j int) bool {
		ri, rj := severityRank(deduped[i].Severity), severityRank(deduped[j].Severity)
		if ri != rj {
			return ri > rj // higher rank first
		}
		if deduped[i].File != deduped[j].File {
			return deduped[i].File < deduped[j].File
		}
		return deduped[i].Line < deduped[j].Line
	})

	// Cap at maxComments
	if len(deduped) > maxComments {
		deduped = deduped[:maxComments]
	}

	// Build combined summary
	combinedSummary := ""
	for _, s := range summaries {
		combinedSummary += s + "\n"
	}

	return &models.ReviewResult{
		Summary:  combinedSummary,
		Comments: deduped,
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
