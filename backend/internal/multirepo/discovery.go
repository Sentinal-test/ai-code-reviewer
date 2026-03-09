package multirepo

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v60/github"
)

// DiscoveredRepo contains metadata about a repository that was discovered and passed filtering.
type DiscoveredRepo struct {
	Owner         string
	Name          string
	FullName      string
	CloneURL      string
	DefaultBranch string
}

// DiscoverRepos filters the raw list of repositories returned by the GitHub App installation.
// It applies the deterministic rules specified in the plan (e.g. exclude archived, disabled, forks, current repo).
func DiscoverRepos(ctx context.Context, allRepos []*github.Repository, currentRepoFullName string) []DiscoveredRepo {
	var discovered []DiscoveredRepo

	currentOwner := ""
	if parts := strings.Split(currentRepoFullName, "/"); len(parts) >= 1 {
		currentOwner = parts[0]
	}

	fmt.Printf("      [Discovery] Evaluating %d repos (current: %s, owner: %s)\n", len(allRepos), currentRepoFullName, currentOwner)

	for _, repo := range allRepos {
		fullName := repo.GetFullName()

		// ❌ Exclude the current PR repo itself
		if strings.EqualFold(fullName, currentRepoFullName) {
			fmt.Printf("      [Discovery] ❌ %s — skipped (current PR repo)\n", fullName)
			continue
		}

		// ❌ Exclude Archived repos
		if repo.GetArchived() {
			fmt.Printf("      [Discovery] ❌ %s — skipped (archived)\n", fullName)
			continue
		}

		// ❌ Exclude Disabled repos
		if repo.GetDisabled() {
			fmt.Printf("      [Discovery] ❌ %s — skipped (disabled)\n", fullName)
			continue
		}

		// ❌ Exclude Empty repos
		if repo.GetSize() == 0 {
			fmt.Printf("      [Discovery] ❌ %s — skipped (empty, size=0)\n", fullName)
			continue
		}

		// ❌ Exclude Forks (unless explicitly enabled later)
		if repo.GetFork() {
			fmt.Printf("      [Discovery] ❌ %s — skipped (fork)\n", fullName)
			continue
		}

		// ✅ Security Scope: Only include repos from the same owner (Organizational boundary)
		repoOwner := repo.GetOwner().GetLogin()
		if !strings.EqualFold(repoOwner, currentOwner) {
			fmt.Printf("      [Discovery] ❌ %s — skipped (different owner: %s != %s)\n", fullName, repoOwner, currentOwner)
			continue
		}

		fmt.Printf("      [Discovery] ✅ %s — included\n", fullName)
		discovered = append(discovered, DiscoveredRepo{
			Owner:         repoOwner,
			Name:          repo.GetName(),
			FullName:      fullName,
			CloneURL:      repo.GetCloneURL(),
			DefaultBranch: repo.GetDefaultBranch(),
		})
	}

	return discovered
}
