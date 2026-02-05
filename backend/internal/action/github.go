package action

import (
	"code-review/backend/internal/models"
	"context"
	"fmt"
	"strings"

	"time"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

type GitHubClient struct {
	client *github.Client
	owner  string
	repo   string
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
	}
}

// PostReviewComment posts the review comments to the PR.
// It tries to group them into a single review if possible, or posts individual comments.
// PostReview posts the review comments to the PR.
// It matches the robustness of the SaaS backend by implementing a fallback strategy.
func (g *GitHubClient) PostReview(ctx context.Context, prNumber int, result *models.ReviewResult, commitSHA string) error {
	if result == nil {
		return nil
	}

	// 1. Try Batched Review (Best for UI/Noise)
	err := g.postBatchedReview(ctx, prNumber, result, commitSHA)
	if err == nil {
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

		msg := fmt.Sprintf("**[%s]** %s\n\n%s", strings.ToUpper(c.Severity), c.Layer, c.Message)

		comment := &github.PullRequestComment{
			Body:     github.String(msg),
			Path:     github.String(c.File),
			Line:     github.Int(c.Line),
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

	// Post the final summary
	summaryMsg := fmt.Sprintf("### AI Code Review Summary\n\n%s", result.Summary)
	if failedInline > 0 {
		summaryMsg += fmt.Sprintf("\n\n---\n*Note: %d comments were posted as general comments because their line numbers could not be resolved in the PR diff.*", failedInline)
	}
	g.postGeneralComment(ctx, prNumber, summaryMsg)

	fmt.Printf("✅ Fallback Review Complete | Posted %d/%d comments successfully\n", successCount, len(result.Comments))
	return nil
}

func (g *GitHubClient) postBatchedReview(ctx context.Context, prNumber int, result *models.ReviewResult, commitSHA string) error {
	var comments []*github.DraftReviewComment
	for _, c := range result.Comments {
		msg := fmt.Sprintf("**[%s]** %s\n\n%s", strings.ToUpper(c.Severity), c.Layer, c.Message)
		comments = append(comments, &github.DraftReviewComment{
			Path: github.String(c.File),
			Line: github.Int(c.Line),
			Body: github.String(msg),
		})
	}

	if len(comments) == 0 && result.Summary == "" {
		return nil
	}

	reviewRequest := &github.PullRequestReviewRequest{
		CommitID: github.String(commitSHA),
		Event:    github.String("COMMENT"),
		Body:     github.String("### AI Code Review Summary\n\n" + result.Summary),
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
