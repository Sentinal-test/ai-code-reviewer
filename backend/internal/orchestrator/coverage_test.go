package orchestrator

import (
	"fmt"
	"sync"
	"testing"
)

func TestCoverageManifestConcurrentAccess(t *testing.T) {
	manifest := NewCoverageManifest()

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := fmt.Sprintf("file_%d.go", i)
			manifest.AddFile(path, CoverageStatusPending, "queued")
			manifest.UpdateStatus(path, CoverageStatusReviewed, "")
			_ = manifest.GenerateSummary()
		}()
	}

	wg.Wait()

	if got := len(manifest.Files); got != 64 {
		t.Fatalf("expected 64 files in manifest, got %d", got)
	}
}
