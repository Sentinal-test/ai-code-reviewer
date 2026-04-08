package main

import (
	"code-review/backend/internal/action"
	"context"
	"os"
	"testing"
)

func TestGitHubClientInitialization(t *testing.T) {
	// 1. Create token-based client
	tokenClient := action.NewGitHubClient(context.Background(), "fake-token", "owner", "repo")
	if tokenClient == nil {
		t.Fatalf("Expected token client to be created")
	}

	// 2. Validate App-based client with missing / bad string
	_, err := action.NewGitHubAppClient(context.Background(), 12345, "invalid-key-string", "owner", "repo")
	if err == nil {
		t.Fatalf("Expected error for invalid private key format")
	}

	// 3. Test multi-repo availability bypass logic
	isMulti := checkMultiRepoAvailability(context.Background(), nil)
	if isMulti {
		t.Fatalf("Expected nil client to return false for multi-repo")
	}
}

func TestDetectSCMProvider(t *testing.T) {
	originalMR := os.Getenv("CI_MERGE_REQUEST_IID")
	originalGitLab := os.Getenv("GITLAB_CI")
	originalSCM := os.Getenv("SCM_PROVIDER")
	defer func() {
		_ = os.Setenv("CI_MERGE_REQUEST_IID", originalMR)
		_ = os.Setenv("GITLAB_CI", originalGitLab)
		_ = os.Setenv("SCM_PROVIDER", originalSCM)
	}()

	_ = os.Unsetenv("CI_MERGE_REQUEST_IID")
	_ = os.Unsetenv("GITLAB_CI")
	_ = os.Unsetenv("SCM_PROVIDER")
	if got := detectSCMProvider(""); got != "github" {
		t.Fatalf("expected github default, got %s", got)
	}

	_ = os.Setenv("CI_MERGE_REQUEST_IID", "42")
	if got := detectSCMProvider(""); got != "gitlab" {
		t.Fatalf("expected gitlab from CI vars, got %s", got)
	}

	if got := detectSCMProvider("github"); got != "github" {
		t.Fatalf("expected explicit flag to win, got %s", got)
	}
}
