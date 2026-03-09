package multirepo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ShallowCheckout clones the default branch of a given repo into targetDir using the provided token.
func ShallowCheckout(ctx context.Context, repo DiscoveredRepo, token, targetDir string) error {
	// Securely construct the clone URL with the token
	// cloneURL is typically "https://github.com/owner/repo.git"
	// We want "https://x-access-token:<token>@github.com/owner/repo.git"
	authURL := strings.Replace(repo.CloneURL, "https://", fmt.Sprintf("https://x-access-token:%s@", token), 1)

	// Ensure the target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target dir %s: %w", targetDir, err)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", repo.DefaultBranch, authURL, targetDir)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed for %s: %w", repo.FullName, err)
	}

	return nil
}
