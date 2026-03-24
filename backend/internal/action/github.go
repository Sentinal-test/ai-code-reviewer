package action

import (
	"code-review/backend/internal/models"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"code-review/backend/internal/memory"
	"math"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

// hunkHeaderRegex matches unified diff hunk headers like @@ -10,5 +12,8 @@
var hunkHeaderRegex = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// extractValidDiffLines parses a unified diff and returns a map of
// file path → sorted slice of valid line numbers on the new-file (RIGHT) side.
// Only lines that appear as added (+) or context (unchanged) within hunks are valid
// for GitHub inline comments.
func extractValidDiffLines(diff string) map[string][]int {
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

// snapToValidLine finds the nearest valid diff line for a given line number.
// Returns 0 if no valid lines exist for the file.
func snapToValidLine(line int, validLines []int) int {
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

type GitHubClient struct {
	client *github.Client
	owner  string
	repo   string
	Token  string
}

func NewGitHubClient(ctx context.Context, token, owner, repo string) *GitHubClient {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	return &GitHubClient{
		client: client,
		owner:  owner,
		repo:   repo,
		Token:  token,
	}
}

func (g *GitHubClient) GetRawClient() *github.Client {
	return g.client
}

// NewGitHubAppClient initializes a GitHub client using an App Installation Token.
// It authenticates as the App, finds its installation for the repo, and creates a client.
func NewGitHubAppClient(ctx context.Context, appID int64, privateKeyString, owner, repo string) (*GitHubClient, error) {
	// Parse private key. If it's a file path or raw string, we'll try raw bytes first.
	var privateKey []byte
	// if it doesn't look like an RSA key header, might be a file path, though we expect raw string from Secrets.
	if !strings.Contains(privateKeyString, "-----BEGIN") {
		// Just in case it's a path (unlikely for Secrets, but good fallback)
		data, err := os.ReadFile(privateKeyString)
		if err == nil {
			privateKey = data
		} else {
			privateKey = []byte(privateKeyString)
		}
	} else {
		privateKey = []byte(privateKeyString)
	}

	// Create App transport
	appTransport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appID, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create app transport: %w", err)
	}

	// Temporary client just to find the installation ID
	appClient := github.NewClient(&http.Client{Transport: appTransport})

	install, _, err := appClient.Apps.FindRepositoryInstallation(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to find installation for %s/%s: %w", owner, repo, err)
	}

	// Create Installation transport
	itr := ghinstallation.NewFromAppsTransport(appTransport, install.GetID())
	installClient := github.NewClient(&http.Client{Transport: itr})

	// Fetch actual token string to store manually if needed by scripts
	token, _, err := appClient.Apps.CreateInstallationToken(ctx, install.GetID(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating installation token: %w", err)
	}
	rawToken := ""
	if token != nil {
		rawToken = token.GetToken()
	}

	return &GitHubClient{
		client: installClient,
		owner:  owner,
		repo:   repo,
		Token:  rawToken, // Store the raw token for things that need to clone
	}, nil
}

// PostReviewComment posts the review comments to the PR.
// It tries to group them into a single review if possible, or posts individual comments.
// PostReview posts the review comments to the PR.
// It matches the robustness of the SaaS backend by implementing a fallback strategy.
func (g *GitHubClient) PostReview(ctx context.Context, prNumber int, result *models.ReviewResult, commitSHA string, diff string, commentNodeMap map[int64]string) error {
	if result == nil {
		return nil
	}

	// Pre-compute valid diff lines for line snapping
	validLines := extractValidDiffLines(diff)

	// 1. Try Batched Review (Best for UI/Noise)
	err := g.postBatchedReview(ctx, prNumber, result, commitSHA, validLines)
	if err == nil {
		// Resolve GitHub review threads for findings the Consolidator marked as resolved
		ResolveThreads(g.Token, result.Resolutions, commentNodeMap)
		return nil
	}

	// 2. Identify 422 Errors (Invalid Lines)
	// If the error is not 422, it might be a connectivity issue, but we can still try the fallback loop
	// just in case it helps (e.g. partial success).
	// The specific error from GitHub for invalid lines usually contains "422" or "Validation Failed".
	isValidationErr := strings.Contains(err.Error(), "422") || strings.Contains(err.Error(), "Validation Failed")

	if !isValidationErr {
		// If it's a critical API error (auth, rate limit), failing hard might be better,
		// but let's log and try fallback as a best-effort recovery.
		fmt.Printf("⚠️ Batched review failed with non-validation error: %v. Attempting fallback...\n", err)
	} else {
		fmt.Printf("⚠️ Batched review failed with validation error (likely invalid lines): %v. Switching to robust individual posting.\n", err)
	}

	// 3. Fallback: Individual Posting (Matching SaaS Logic)
	// If batched fails, we post comments one by one. If an individual comment fails (e.g. invalid line),
	// we fall back to a General PR Comment for that specific item.

	successCount := 0
	failedInline := 0

	for i, c := range result.Comments {
		// Periodically sleep to avoid secondary rate limits (GitHub abuse protection)
		if i > 0 && i%5 == 0 {
			time.Sleep(2 * time.Second)
		}

		// Skip comments with invalid line numbers — post as general comment instead
		if c.Line <= 0 {
			fmt.Printf("  ⚠️ Skipping inline comment on %s:L%d (invalid line) — posting as general comment\n", c.File, c.Line)
			fallbackMsg := fmt.Sprintf("⚠️ **Review comment for %s** (could not resolve line number)\n\n**[%s]** %s\n\n%s", c.File, strings.ToUpper(c.Severity), c.Layer, c.Message)
			genErr := g.postGeneralComment(ctx, prNumber, fallbackMsg)
			if genErr == nil {
				successCount++
			}
			continue
		}

		// Snap line number to nearest valid diff line
		snappedLine := c.Line
		if fileLines, ok := validLines[c.File]; ok {
			snappedLine = snapToValidLine(c.Line, fileLines)
			if snappedLine == 0 {
				fmt.Printf("  ⚠️ No valid diff lines for %s — posting as general comment\n", c.File)
				fallbackMsg := fmt.Sprintf("⚠️ **Review comment for %s:L%d** (line not in diff)\n\n**[%s]** %s\n\n%s",
					c.File, c.Line, strings.ToUpper(c.Severity), c.Layer, c.Message)
				genErr := g.postGeneralComment(ctx, prNumber, fallbackMsg)
				if genErr == nil {
					successCount++
				}
				continue
			}
			if snappedLine != c.Line {
				fmt.Printf("  📌 Snapped %s:L%d → L%d (nearest valid diff line)\n", c.File, c.Line, snappedLine)
			}
		}

		encodedFile := base64.RawURLEncoding.EncodeToString([]byte(c.File))
		marker := fmt.Sprintf("<!-- ai-reviewer:v1 file=%s line=%d severity=%s layer=%s hash=%s -->",
			encodedFile, c.Line, c.Severity, c.Layer, memory.Fingerprint(c.File, c.Layer, c.Message))
		msg := marker + "\n" + fmt.Sprintf("**[%s]** %s\n\n%s", strings.ToUpper(c.Severity), c.Layer, c.Message)

		comment := &github.PullRequestComment{
			Body:     github.String(msg),
			Path:     github.String(c.File),
			Line:     github.Int(snappedLine),
			Side:     github.String("RIGHT"),
			CommitID: github.String(commitSHA),
		}

		err := g.postComment(ctx, prNumber, comment)
		if err != nil {
			// Check if we hit a rate limit
			if strings.Contains(err.Error(), "403") && (strings.Contains(err.Error(), "secondary rate limit") || strings.Contains(err.Error(), "temporarily blocked")) {
				fmt.Printf("🛑 Hit GitHub Secondary Rate Limit. Aborting individual posts and dumping remaining as one summary.\n")
				// Post remaining comments as a single bulk comment to avoid further rate limit issues
				var remaining strings.Builder
				remaining.WriteString("### Remaining Review Comments (Delayed due to Rate Limits)\n\n")
				for j := i; j < len(result.Comments); j++ {
					remC := result.Comments[j]
					remaining.WriteString(fmt.Sprintf("- **%s** (%s:%d): %s\n", strings.ToUpper(remC.Severity), remC.File, remC.Line, remC.Message))
				}
				g.postGeneralComment(ctx, prNumber, remaining.String())
				break
			}

			// If individual comment fails (likely 422 again), post as General Comment
			if strings.Contains(err.Error(), "422") || strings.Contains(err.Error(), "Validation Failed") {
				fmt.Printf("  ⚠️ Failed to post inline comment on %s:L%d. Retrying as General Comment..\n", c.File, c.Line)
				failedInline++

				fallbackMsg := fmt.Sprintf("⚠️ **Could not post inline on %s:L%d** (Line mismatch in diff)\n\n%s", c.File, c.Line, msg)
				genErr := g.postGeneralComment(ctx, prNumber, fallbackMsg)
				if genErr == nil {
					successCount++
				}
			} else {
				fmt.Printf("  ❌ Failed to post comment on %s:L%d: %v\n", c.File, c.Line, err)
			}
		} else {
			successCount++
		}

		// Small delay to be polite to the API
		time.Sleep(500 * time.Millisecond)
	}

	// Build resolved issues block and append to summary
	resBlock := buildResolutionsBlock(result.Resolutions)
	summaryMsg := fmt.Sprintf("### AI Code Review Summary\n\n%s%s", result.Summary, resBlock)
	if failedInline > 0 {
		summaryMsg += fmt.Sprintf("\n\n---\n*Note: %d comments were posted as general comments because their line numbers could not be resolved in the PR diff.*", failedInline)
	}
	g.postGeneralComment(ctx, prNumber, summaryMsg)

	// Resolve GitHub review threads for findings the Consolidator marked as resolved
	ResolveThreads(g.Token, result.Resolutions, commentNodeMap)

	fmt.Printf("✅ Fallback Review Complete | Posted %d/%d comments successfully\n", successCount, len(result.Comments))
	return nil
}

func (g *GitHubClient) postBatchedReview(ctx context.Context, prNumber int, result *models.ReviewResult, commitSHA string, validLines map[string][]int) error {
	var comments []*github.DraftReviewComment
	var generalComments []models.ReviewComment
	for _, c := range result.Comments {
		// Skip invalid lines — collect for general comment posting
		if c.Line <= 0 {
			generalComments = append(generalComments, c)
			continue
		}

		// Validate and snap line to nearest valid diff line
		snappedLine := c.Line
		if fileLines, ok := validLines[c.File]; ok {
			snappedLine = snapToValidLine(c.Line, fileLines)
			if snappedLine == 0 {
				// No valid diff lines for this file — post as general
				generalComments = append(generalComments, c)
				continue
			}
		}

		encodedFile := base64.RawURLEncoding.EncodeToString([]byte(c.File))
		marker := fmt.Sprintf("<!-- ai-reviewer:v1 file=%s line=%d severity=%s layer=%s hash=%s -->",
			encodedFile, c.Line, c.Severity, c.Layer, memory.Fingerprint(c.File, c.Layer, c.Message))
		msg := marker + "\n" + fmt.Sprintf("**[%s]** %s\n\n%s", strings.ToUpper(c.Severity), c.Layer, c.Message)
		comments = append(comments, &github.DraftReviewComment{
			Path: github.String(c.File),
			Line: github.Int(snappedLine),
			Body: github.String(msg),
		})
	}

	// Post L0 comments as general comments
	for _, c := range generalComments {
		msg := fmt.Sprintf("⚠️ **Review comment for %s** (could not resolve line number)\n\n**[%s]** %s\n\n%s",
			c.File, strings.ToUpper(c.Severity), c.Layer, c.Message)
		g.postGeneralComment(ctx, prNumber, msg)
	}

	resBlock := buildResolutionsBlock(result.Resolutions)

	if len(comments) == 0 && result.Summary == "" && resBlock == "" {
		return nil
	}

	reviewRequest := &github.PullRequestReviewRequest{
		CommitID: github.String(commitSHA),
		Event:    github.String("COMMENT"),
		Body:     github.String("### AI Code Review Summary\n\n" + result.Summary + resBlock),
		Comments: comments,
	}

	_, _, err := g.client.PullRequests.CreateReview(ctx, g.owner, g.repo, prNumber, reviewRequest)
	return err
}

func (g *GitHubClient) postComment(ctx context.Context, prNumber int, comment *github.PullRequestComment) error {
	_, _, err := g.client.PullRequests.CreateComment(ctx, g.owner, g.repo, prNumber, comment)
	return err
}

func (g *GitHubClient) postGeneralComment(ctx context.Context, prNumber int, body string) error {
	comment := &github.IssueComment{
		Body: github.String(body),
	}
	_, _, err := g.client.Issues.CreateComment(ctx, g.owner, g.repo, prNumber, comment)
	return err
}

// GetPullRequest fetches the PR metadata (Title, Body).
func (g *GitHubClient) GetPullRequest(ctx context.Context, prNumber int) (*models.PRContext, error) {
	pr, _, err := g.client.PullRequests.Get(ctx, g.owner, g.repo, prNumber)
	if err != nil {
		return nil, err
	}

	var commitMsgs []string
	opts := &github.ListOptions{
		PerPage: 100, // Fetch all commits, we'll take the last 2
	}

	commits, _, commitErr := g.client.PullRequests.ListCommits(ctx, g.owner, g.repo, prNumber, opts)
	if commitErr == nil {
		// Take the last 2 commits (most recent)
		startIdx := 0
		if len(commits) > 2 {
			startIdx = len(commits) - 2
		}

		for i := startIdx; i < len(commits); i++ {
			if commits[i].Commit != nil && commits[i].Commit.Message != nil {
				commitMsgs = append(commitMsgs, *commits[i].Commit.Message)
			}
		}
	} else {
		fmt.Printf("⚠️ Failed to fetch commits for PR %d: %v\n", prNumber, commitErr)
	}

	return &models.PRContext{
		Title:          pr.GetTitle(),
		Body:           pr.GetBody(),
		CommitMessages: commitMsgs,
	}, nil
}

// ListAccessibleRepos fetches all repositories accessible to the current GitHub App installation.
func (g *GitHubClient) ListAccessibleRepos(ctx context.Context) ([]*github.Repository, error) {
	var allRepos []*github.Repository
	opts := &github.ListOptions{PerPage: 100}

	for {
		result, resp, err := g.client.Apps.ListRepos(ctx, opts)
		if err != nil {
			return nil, err
		}

		allRepos = append(allRepos, result.Repositories...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

// buildResolutionsBlock formats the merged resolutions, safely escaping the LLM output.
func buildResolutionsBlock(resolutions []models.Resolution) string {
	var resBlock strings.Builder
	resolvedCount := 0
	for _, res := range resolutions {
		if strings.EqualFold(res.Status, "resolved") {
			if resolvedCount == 0 {
				resBlock.WriteString("\n\n### ✅ Resolved Issues\n")
			}
			// Replace newlines and carriage returns with spaces to prevent markdown list breakage
			safeReason := strings.ReplaceAll(res.Reason, "\r\n", " ")
			safeReason = strings.ReplaceAll(safeReason, "\n", " ")
			safeReason = strings.ReplaceAll(safeReason, "\r", " ")
			resBlock.WriteString(fmt.Sprintf("- %s\n", safeReason))
			resolvedCount++
		}
	}
	return resBlock.String()
}
