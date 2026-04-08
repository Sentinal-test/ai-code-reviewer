package github

import (
	"code-review/backend/internal/models"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"code-review/backend/internal/memory"
	"code-review/backend/internal/platform/utils"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

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

func (g *GitHubClient) Provider() string {
	return "github"
}

func (g *GitHubClient) FetchPreviousFindings(ctx context.Context, id int) ([]models.PreviousFinding, error) {
	return memory.FetchPreviousFindings(ctx, g.client, g.owner, g.repo, id)
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

// PostReview posts the review comments to the PR.
// It matches the robustness of the SaaS backend by implementing a fallback strategy.
func (g *GitHubClient) PostReview(ctx context.Context, prNumber int, result *models.ReviewResult, commitSHA string, diff string, previous []models.PreviousFinding) error {
	if result == nil {
		return nil
	}

	commentNodeMap := BuildCommentNodeMap(previous)

	// Pre-compute valid diff lines for line snapping
	validLines := utils.ExtractValidDiffLines(diff)

	// 1. Try Batched Review (Best for UI/Noise)
	err := g.postBatchedReview(ctx, prNumber, result, commitSHA, validLines)
	if err == nil {
		// Resolve GitHub review threads for findings the Consolidator marked as resolved
		ResolveThreads(g.Token, g.owner, g.repo, prNumber, result.Resolutions, commentNodeMap)
		return nil
	}

	// 2. Identify 422 Errors (Invalid Lines)
	// If the error is not 422, it might be a connectivity issue, but we can still try the fallback loop
	// just in case it helps (e.g. partial success).
	isValidationErr := strings.Contains(err.Error(), "422") || strings.Contains(err.Error(), "Validation Failed")

	if !isValidationErr {
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
			fallbackMsg := fmt.Sprintf("⚠️ **Review comment for %s** (could not resolve line number)\n\n%s", c.File, memory.BuildMarkerCommentBody(c))
			genErr := g.postGeneralComment(ctx, prNumber, fallbackMsg)
			if genErr == nil {
				successCount++
			}
			continue
		}

		// Snap line number to nearest valid diff line
		snappedLine := c.Line
		if fileLines, ok := validLines[c.File]; ok {
			snappedLine = utils.SnapToValidLine(c.Line, fileLines)
			if snappedLine == 0 {
				fmt.Printf("  ⚠️ No valid diff lines for %s — posting as general comment\n", c.File)
				fallbackMsg := fmt.Sprintf("⚠️ **Review comment for %s:L%d** (line not in diff)\n\n%s",
					c.File, c.Line, memory.BuildMarkerCommentBody(c))
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

		msg := memory.BuildMarkerCommentBody(c)

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

		time.Sleep(500 * time.Millisecond)
	}

	// Build resolved issues block and append to summary
	resBlock := memory.BuildResolutionsBlock(result.Resolutions)
	summaryMsg := fmt.Sprintf("### AI Code Review Summary\n\n%s%s", result.Summary, resBlock)
	if failedInline > 0 {
		summaryMsg += fmt.Sprintf("\n\n---\n*Note: %d comments were posted as general comments because their line numbers could not be resolved in the PR diff.*", failedInline)
	}
	g.postGeneralComment(ctx, prNumber, summaryMsg)

	// Resolve GitHub review threads for findings the Consolidator marked as resolved
	ResolveThreads(g.Token, g.owner, g.repo, prNumber, result.Resolutions, commentNodeMap)

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
			snappedLine = utils.SnapToValidLine(c.Line, fileLines)
			if snappedLine == 0 {
				// No valid diff lines for this file — post as general
				generalComments = append(generalComments, c)
				continue
			}
		}

		msg := memory.BuildMarkerCommentBody(c)
		comments = append(comments, &github.DraftReviewComment{
			Path: github.String(c.File),
			Line: github.Int(snappedLine),
			Body: github.String(msg),
		})
	}

	// Post L0 comments as general comments
	for _, c := range generalComments {
		msg := fmt.Sprintf("⚠️ **Review comment for %s** (could not resolve line number)\n\n%s",
			c.File, memory.BuildMarkerCommentBody(c))
		g.postGeneralComment(ctx, prNumber, msg)
	}

	resBlock := memory.BuildResolutionsBlock(result.Resolutions)

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
		// Take the last 5 commits (most recent)
		startIdx := 0
		if len(commits) > 5 {
			startIdx = len(commits) - 5
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
