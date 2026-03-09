package multirepo

import (
	"context"
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

	for _, repo := range allRepos {
		// ❌ Exclude the current PR repo itself
		if strings.EqualFold(repo.GetFullName(), currentRepoFullName) {
			continue
		}

		// ❌ Exclude Archived repos
		if repo.GetArchived() {
			continue
		}

		// ❌ Exclude Disabled repos
		if repo.GetDisabled() {
			continue
		}

		// ❌ Exclude Empty repos
		if repo.GetSize() == 0 {
			continue
		}

		// ❌ Exclude Forks (unless explicitly enabled later)
		if repo.GetFork() {
			continue
		}

		// ✅ Security Scope: Only include repos from the same owner (Organizational boundary)
		currentOwner := strings.Split(currentRepoFullName, "/")[0]
		if !strings.EqualFold(repo.GetOwner().GetLogin(), currentOwner) {
			continue
		}

		discovered = append(discovered, DiscoveredRepo{
			Owner:         repo.GetOwner().GetLogin(),
			Name:          repo.GetName(),
			FullName:      repo.GetFullName(),
			CloneURL:      repo.GetCloneURL(),
			DefaultBranch: repo.GetDefaultBranch(),
		})
	}

	return discovered
}
