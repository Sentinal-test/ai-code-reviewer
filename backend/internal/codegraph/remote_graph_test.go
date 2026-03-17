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
import "github.com/acme/shared/auth"
func RemoteHelper() {
	auth.ValidateToken()
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

	if len(fileEntry.Imports) != 1 || fileEntry.Imports[0] != "github.com/acme/shared/auth" {
		t.Errorf("expected shared import, got %v", fileEntry.Imports)
	}

	foundFunc := false
	for _, def := range fileEntry.Definitions {
		if def.Symbol == "RemoteHelper" && def.Kind == "function" && def.QualifiedSymbol == "main.RemoteHelper" {
			foundFunc = true
			break
		}
	}
	if !foundFunc {
		t.Errorf("expected to find function RemoteHelper in definitions")
	}

	if graph.Symbols["main.RemoteHelper"].QualifiedSymbol == "" {
		t.Fatalf("expected remote graph symbols to contain main.RemoteHelper")
	}

	if len(fileEntry.References) != 1 || fileEntry.References[0].Symbol != "ValidateToken" {
		t.Fatalf("expected external reference to ValidateToken, got %+v", fileEntry.References)
	}

	if fileEntry.ImportAliases["auth"] != "github.com/acme/shared/auth" {
		t.Fatalf("expected import alias auth to resolve, got %v", fileEntry.ImportAliases)
	}

	if len(graph.SymbolEdges) != 0 {
		t.Fatalf("expected no internal symbol edges for pure external call, got %+v", graph.SymbolEdges)
	}
}
