package platform

import (
	"context"

	"code-review/backend/internal/models"
)

// ReviewPlatform defines the contract for interacting with any source code management provider.
type ReviewPlatform interface {
	// Provider returns the name of the provider (e.g., "github", "gitlab").
	Provider() string

	// GetPullRequest fetches the metadata (title, body, commit messages) for a PR/MR.
	GetPullRequest(ctx context.Context, id int) (*models.PRContext, error)

	// FetchPreviousFindings loads previously posted review comments for deduplication and resolution.
	FetchPreviousFindings(ctx context.Context, id int) ([]models.PreviousFinding, error)

	// PostReview posts the generated review comments to the PR/MR.
	PostReview(ctx context.Context, id int, result *models.ReviewResult, commitSHA string, diff string, previous []models.PreviousFinding) error
}
