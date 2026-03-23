package memory

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"code-review/backend/internal/models"
	"github.com/google/go-github/v60/github"
)

// PreviousFinding represents a comment the bot previously posted.
type PreviousFinding struct {
	File      string
	Line      int
	Severity  string
	Layer     string
	Message   string
	Hash      string // fingerprint for dedup
	CommentID int64  // GitHub comment ID
}

// markerRegex extracts fields from the hidden HTML marker.
// Format: <!-- ai-reviewer:v1 file=path/to/file.go line=42 severity=critical layer=bug hash=abc123 -->
var markerRegex = regexp.MustCompile(`<!-- ai-reviewer:v1 file=([^ ]+) line=([^ ]+) severity=([^ ]+) layer=([^ ]+) hash=([^ ]+) -->`)

// Fingerprint computes a stable deduplication hash for a finding.
// It normalizes the message (lowercasing, trimming) and considers the file,
// layer, and line number (to a degree, though the ±5 line check happens later).
func Fingerprint(file, layer, message string) string {
	// Normalize the message: lowercase, strip extra whitespace
	normalizedMsg := strings.ToLower(strings.TrimSpace(message))
	normalizedMsg = strings.ReplaceAll(normalizedMsg, " ", "")
	normalizedMsg = strings.ReplaceAll(normalizedMsg, "\n", "")

	// Combine components
	data := fmt.Sprintf("%s|%s|%s", file, layer, normalizedMsg)
	
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])[:12] // 12 chars is plenty for dedup
}

// ParseMarker extracts PreviousFinding data from a string containing the marker.
// Returns nil if no valid marker is found.
func ParseMarker(body, actualFile string, actualLine int, commentID int64) *PreviousFinding {
	matches := markerRegex.FindStringSubmatch(body)
	if len(matches) < 6 {
		return nil // Not our bot's comment or malformed
	}

	file := matches[1]
	if fileBytes, err := base64.RawURLEncoding.DecodeString(file); err == nil && len(fileBytes) > 0 {
		file = string(fileBytes)
	}

	lineStr := matches[2]
	severity := matches[3]
	layer := matches[4]
	hash := matches[5]

	line, _ := strconv.Atoi(lineStr)

	// Fallbacks: If the marker claims a different file/line than what GitHub returned 
	// (usually impossible unless manually edited), trust the marker as intent.
	if file == "" {
		file = actualFile
	}
	if line <= 0 {
		line = actualLine
	}

	return &PreviousFinding{
		File:      file,
		Line:      line,
		Severity:  severity,
		Layer:     layer,
		Message:   body, // We don't need the exact original message for dedup, only the hash
		Hash:      hash,
		CommentID: commentID,
	}
}

// FetchPreviousFindings calls GitHub API to list all review comments on the PR,
// filters to only our bot's comments, and parses them into a list.
func FetchPreviousFindings(ctx context.Context, client *github.Client, owner, repo string, prNumber int) ([]PreviousFinding, error) {
	var findings []PreviousFinding
	opts := &github.PullRequestListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}}

	for {
		comments, resp, err := client.PullRequests.ListComments(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("listing PR comments: %w", err)
		}

		for _, c := range comments {
			if c.Body == nil {
				continue
			}

			// In PR review comments, the line could be `Line` or `OriginalLine`
			// Usually we only care about the latest line, but the marker holds the original intent.
			actualFile := ""
			if c.Path != nil {
				actualFile = *c.Path
			}
			actualLine := 0
			if c.Line != nil {
				actualLine = *c.Line
			}

			if pf := ParseMarker(*c.Body, actualFile, actualLine, c.GetID()); pf != nil {
				findings = append(findings, *pf)
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	// 2. Fetch general PR issue comments (the fallback for when diff lines don't match)
	issueOpts := &github.IssueListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	for {
		comments, resp, err := client.Issues.ListComments(ctx, owner, repo, prNumber, issueOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to list issue comments: %w", err)
		}

		for _, c := range comments {
			if c.Body == nil {
				continue
			}
			if marker := ParseMarker(*c.Body, "", 0, c.GetID()); marker != nil {
				findings = append(findings, *marker)
			}
		}

		if resp.NextPage == 0 {
			break
		}
		issueOpts.Page = resp.NextPage
	}

	return findings, nil
}

// Deduplicate filters new findings against previous ones.
// Returns only genuinely new findings that should be posted, and the count of skipped ones.
func Deduplicate(newFindings []models.ReviewComment, previous []PreviousFinding) ([]models.ReviewComment, int) {
	var toPost []models.ReviewComment
	var skipped int

	for _, newFinding := range newFindings {
		isDuplicate := false
		newHash := Fingerprint(newFinding.File, newFinding.Layer, newFinding.Message)

		for _, prevFinding := range previous {
			// Fast path: Exact fingerprint match (same file, layer, and normalized message content)
			if newHash == prevFinding.Hash {
				// We also require lines to be somewhat close to avoid catching identical 
				// boilerplate violations in vastly different parts of the same file.
				// Although practically if it's the exact same message in the same file, 
				// skipping it is usually fine. Let's enforce ±10 lines for safety.
				lineDiff := newFinding.Line - prevFinding.Line
				if lineDiff < 0 {
					lineDiff = -lineDiff
				}
				
				if lineDiff <= 10 {
					isDuplicate = true
					break
				}
			}

			// Fallback path: same file, VERY close line (±3), same layer.
			// To avoid dropping distinct bugs on adjacent lines, verify the message shares context.
			if newFinding.File == prevFinding.File && newFinding.Layer == prevFinding.Layer {
				lineDiff := newFinding.Line - prevFinding.Line
				if lineDiff < 0 {
					lineDiff = -lineDiff
				}
				
				if lineDiff <= 3 {
					// Check message similarity (e.g., sharing a rare word or general substring logic)
					// Simple heuristic: if the first 20 chars match, or one contains the other.
					normalizedNew := strings.ToLower(strings.TrimSpace(newFinding.Message))
					normalizedPrev := strings.ToLower(strings.TrimSpace(prevFinding.Message))
					if strings.Contains(normalizedNew, normalizedPrev) || strings.Contains(normalizedPrev, normalizedNew) {
						isDuplicate = true
						break
					}
					// Also check if they share a significant prefix.
					if len(normalizedNew) > 20 && len(normalizedPrev) > 20 && normalizedNew[:20] == normalizedPrev[:20] {
						isDuplicate = true
						break
					}
				}
			}
		}

		if !isDuplicate {
			toPost = append(toPost, newFinding)
		} else {
			fmt.Printf("  🧠 [Memory] Skipping duplicate finding in %s:L%d (Hash: %s)\n", newFinding.File, newFinding.Line, newHash)
			skipped++
		}
	}

	return toPost, skipped
}
