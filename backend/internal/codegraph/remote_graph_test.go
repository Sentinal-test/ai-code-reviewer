package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildRemoteGraph(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "remote-graph-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcCode := `package main
import "fmt"
func RemoteHelper() {
	fmt.Println("remote")
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "helper.go"), []byte(srcCode), 0644)
	if err != nil {
		t.Fatal(err)
	}

	graph, err := BuildRemoteGraph(context.Background(), "org/remote-repo", tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if graph.RepoFullName != "org/remote-repo" {
		t.Errorf("expected org/remote-repo, got %s", graph.RepoFullName)
	}

	fileEntry, ok := graph.Files["helper.go"]
	if !ok {
		t.Fatalf("expected helper.go in graph")
	}

	if fileEntry.Language != "go" {
		t.Errorf("expected go, got %s", fileEntry.Language)
	}

	if len(fileEntry.Imports) != 1 || fileEntry.Imports[0] != "fmt" {
		t.Errorf("expected [fmt], got %v", fileEntry.Imports)
	}

	foundFunc := false
	for _, def := range fileEntry.Definitions {
		if def.Symbol == "RemoteHelper" && def.Kind == "function" {
			foundFunc = true
			break
		}
	}
	if !foundFunc {
		t.Errorf("expected to find function RemoteHelper in definitions")
	}
}
