package main

import (
	"code-review/backend/internal/action"
	"context"
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
