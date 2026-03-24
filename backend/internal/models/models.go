package models

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// FlexInt64 allows unmarshaling an integer from either a JSON integer or a JSON string.
// This is critical because LLMs may drift and output stringified integers.
type FlexInt64 int64

func (fi *FlexInt64) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			*fi = 0
			return nil
		}
		val, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			*fi = 0
			return nil
		}
		*fi = FlexInt64(val)
		return nil
	}
	var val int64
	if err := json.Unmarshal(b, &val); err != nil {
		*fi = 0
		return nil
	}
	*fi = FlexInt64(val)
	return nil
}

type User struct {
	ID                int
	GitHubID          string
	LLMAPIKey         string
	GitHubAccessToken string
}

type RepoSettings struct {
	ID                  int    `json:"id"`
	RepoID              string `json:"repo_id"`
	UserID              int    `json:"user_id"`
	IsActive            bool   `json:"is_active"`
	SecurityEnabled     bool   `json:"security_enabled"`
	BugEnabled          bool   `json:"bug_enabled"`
	LintEnabled         bool   `json:"lint_enabled"`
	PerformanceEnabled  bool   `json:"performance_enabled"`
	ArchitectureEnabled bool   `json:"architecture_enabled"`
}

type ReviewComment struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Layer    string `json:"layer"`
	Message  string `json:"message"`
}

// Resolution tracks whether a specific previously reported issue was fixed.
type Resolution struct {
	CommentID FlexInt64 `json:"comment_id"`
	Status    string `json:"status"` // "resolved" or "unresolved"
	Reason    string `json:"reason"`
}

type ReviewResult struct {
	Summary     string          `json:"summary"`
	Comments    []ReviewComment `json:"comments"`
	Resolutions []Resolution    `json:"resolutions,omitempty"`
}

// PRContext holds metadata about a PR for enhanced LLM context
type PRContext struct {
	Title          string
	Body           string
	CommitMessages []string
}

// DeveloperRules holds custom review rules defined by repo maintainers.
type DeveloperRules struct {
	Instructions []string `yaml:"instructions"`
	Ignore       []string `yaml:"ignore"`
	Focus        []string `yaml:"focus"`
}

// PreviousFinding represents a comment the bot previously posted.
type PreviousFinding struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Severity  string `json:"severity"`
	Layer     string `json:"layer"`
	Message   string `json:"message"`
	Hash      string    `json:"hash"`       // fingerprint for dedup
	CommentID FlexInt64 `json:"comment_id"` // GitHub comment ID (REST)
	NodeID    string    `json:"node_id"`    // GitHub GraphQL node ID (for thread resolution)
}

// MergeResolutions deduplicates and merges a flat list of resolutions,
// prioritizing the "resolved" status for duplicate comment IDs.
func MergeResolutions(all []Resolution) []Resolution {
	resMap := make(map[FlexInt64]Resolution)
	for _, res := range all {
		if res.CommentID <= 0 {
			continue // skip invalid or hallucinated comment IDs from LLMs
		}
		isResolved := strings.EqualFold(res.Status, "resolved")
		if existing, exists := resMap[res.CommentID]; exists {
			existingResolved := strings.EqualFold(existing.Status, "resolved")
			if !existingResolved && isResolved {
				resMap[res.CommentID] = res
			}
		} else {
			resMap[res.CommentID] = res
		}
	}

	var finalResolutions []Resolution
	for _, res := range resMap {
		finalResolutions = append(finalResolutions, res)
	}

	sort.Slice(finalResolutions, func(i, j int) bool {
		return finalResolutions[i].CommentID < finalResolutions[j].CommentID
	})

	return finalResolutions
}
