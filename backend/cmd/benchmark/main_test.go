package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"code-review/backend/internal/codegraph"
	"code-review/backend/internal/reposelect"
)

func TestCrossRepoRecallScore(t *testing.T) {
	matches := []reposelect.CandidateMatch{
		{RepoFullName: "acme/billing"},
	}

	if got := crossRepoRecallScore([]string{"acme/billing"}, matches); got != 1.0 {
		t.Fatalf("expected full recall, got %.2f", got)
	}

	if got := crossRepoRecallScore(nil, matches); got != 0.0 {
		t.Fatalf("expected unexpected matches to score 0.0, got %.2f", got)
	}

	if got := crossRepoRecallScore(nil, nil); got != 1.0 {
		t.Fatalf("expected empty expectation and no matches to score 1.0, got %.2f", got)
	}
}

func TestMaterializeRemoteRepos(t *testing.T) {
	caseDir := t.TempDir()
	remoteRepoDir := filepath.Join(caseDir, "remote_repos", "acme", "billing")
	if err := os.MkdirAll(remoteRepoDir, 0755); err != nil {
		t.Fatal(err)
	}

	src := `package billing

import "fmt"

func ValidateToken() {
	fmt.Println("ok")
}
`
	if err := os.WriteFile(filepath.Join(remoteRepoDir, "client.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	baseDir, graphs, err := materializeRemoteRepos(context.Background(), caseDir)
	if err != nil {
		t.Fatalf("materializeRemoteRepos returned error: %v", err)
	}
	defer os.RemoveAll(baseDir)

	if _, ok := graphs["acme/billing"]; !ok {
		t.Fatalf("expected acme/billing graph to be present")
	}

	if _, err := os.Stat(filepath.Join(baseDir, "acme", "billing", "client.go")); err != nil {
		t.Fatalf("expected copied remote repo contents, got error: %v", err)
	}
}

func TestMultiRepoDatasetFixtures(t *testing.T) {
	datasetRoot := filepath.Join("..", "..", "..", "datasets", "go", "multi-repo")
	truthPaths, err := filepath.Glob(filepath.Join(datasetRoot, "*", "truth.json"))
	if err != nil {
		t.Fatalf("glob truth files: %v", err)
	}
	if len(truthPaths) == 0 {
		t.Fatalf("no multi-repo dataset cases found under %s", datasetRoot)
	}

	for _, truthPath := range truthPaths {
		truth, err := loadTruth(truthPath)
		if err != nil {
			t.Fatalf("loadTruth(%s): %v", truthPath, err)
		}

		caseDir := filepath.Dir(truthPath)
		t.Run(truth.ID, func(t *testing.T) {
			repoPath, changedFiles, cleanup, err := materializeCaseRepo(caseDir)
			if err != nil {
				t.Fatalf("materializeCaseRepo: %v", err)
			}
			defer cleanup()

			graph, err := codegraph.BuildFull(context.Background(), repoPath)
			if err != nil {
				t.Fatalf("BuildFull: %v", err)
			}

			remoteBaseDir, remoteGraphs, err := materializeRemoteRepos(context.Background(), caseDir)
			if err != nil {
				t.Fatalf("materializeRemoteRepos: %v", err)
			}
			if remoteBaseDir != "" {
				defer os.RemoveAll(remoteBaseDir)
			}

			localSignals := reposelect.ExtractLocalSignals(graph, changedFiles)
			localSignals.ProjectName = reposelect.ExtractProjectName(repoPath)
			localSignals.ProjectTargets = reposelect.ExtractProjectTargets(repoPath)
			matches := reposelect.MatchRepos(localSignals, remoteGraphs)
			score := crossRepoRecallScore(truth.ExpectedCrossRepo, matches)
			if score != 1.0 {
				t.Fatalf("expected full cross-repo recall, got %.2f with matches %+v", score, matches)
			}
		})
	}
}
