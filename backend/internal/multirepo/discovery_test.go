package multirepo

import (
	"context"
	"testing"

	"github.com/google/go-github/v60/github"
)

func ptr[T any](v T) *T { return &v }

func TestDiscoverRepos(t *testing.T) {
	repos := []*github.Repository{
		{
			Owner:    &github.User{Login: ptr("org")},
			Name:     ptr("pr-repo"),
			FullName: ptr("org/pr-repo"),
			Private:  ptr(true),
			Size:     ptr(100),
		},
		{
			Owner:    &github.User{Login: ptr("org")},
			Name:     ptr("archived-repo"),
			FullName: ptr("org/archived-repo"),
			Archived: ptr(true),
			Private:  ptr(true),
			Size:     ptr(100),
		},
		{
			Owner:    &github.User{Login: ptr("org")},
			Name:     ptr("empty-repo"),
			FullName: ptr("org/empty-repo"),
			Private:  ptr(true),
			Size:     ptr(0),
		},
		{
			Owner:      &github.User{Login: ptr("org")},
			Name:       ptr("public-repo"),
			FullName:   ptr("org/public-repo"),
			Private:    ptr(false),
			Visibility: ptr("public"),
			Size:       ptr(100),
		},
		{
			Owner:    &github.User{Login: ptr("org")},
			Name:     ptr("valid-repo"),
			FullName: ptr("org/valid-repo"),
			Private:  ptr(true),
			Size:     ptr(100),
		},
	}

	discovered := DiscoverRepos(context.Background(), repos, "org/pr-repo")

	if len(discovered) != 1 {
		t.Fatalf("expected 1 discovered repo, got %d", len(discovered))
	}

	if discovered[0].FullName != "org/valid-repo" {
		t.Errorf("expected org/valid-repo, got %s", discovered[0].FullName)
	}
}
