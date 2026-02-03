package action

import (
	"code-review/backend/internal/models"
	"context"
	"fmt"
	"strings"

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
func (g *GitHubClient) PostReview(ctx context.Context, prNumber int, result *models.ReviewResult, commitSHA string) error {
	if result == nil {
		return nil
	}

	var comments []*github.DraftReviewComment
	for _, c := range result.Comments {
		// GitHub require position or line. "line" is for the new file.
		// We need to be careful with "line" vs "position" in older APIs, but v60 `DraftReviewComment` uses `Line`.
		// Note: `Line` must be inside the diff. If the line is unchanged, it might fail to comment.
		// For this implementation, we will try to post to the `Line`.

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
		Event:    github.String("COMMENT"), // Or APPROVE/REQUEST_CHANGES based on severity? Sticking to COMMENT for now.
		Body:     github.String("### AI Code Review Summary\n\n" + result.Summary),
		Comments: comments,
	}

	_, _, err := g.client.PullRequests.CreateReview(ctx, g.owner, g.repo, prNumber, reviewRequest)
	if err != nil {
		return fmt.Errorf("failed to create PR review: %w", err)
	}

	return nil
}

// GetPullRequestAuthor fetches the login and email of the PR author.
func (g *GitHubClient) GetPullRequestAuthor(ctx context.Context, prNumber int) (string, string, error) {
	pr, _, err := g.client.PullRequests.Get(ctx, g.owner, g.repo, prNumber)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch PR: %w", err)
	}

	user := pr.GetUser()
	login := user.GetLogin()

	// Fetch full user profile to get email if possible
	fullUser, _, err := g.client.Users.Get(ctx, login)
	email := ""
	if err == nil {
		email = fullUser.GetEmail()
	}

	// Fallback: If profile email is private, check the email in the PR commits
	if email == "" {
		commits, _, err := g.client.PullRequests.ListCommits(ctx, g.owner, g.repo, prNumber, &github.ListOptions{PerPage: 5})
		if err == nil && len(commits) > 0 {
			// Get email from the first commit in the PR
			email = commits[0].GetCommit().GetAuthor().GetEmail()
		}
	}

	return login, email, nil
}
