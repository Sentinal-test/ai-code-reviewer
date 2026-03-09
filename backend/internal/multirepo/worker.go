package multirepo

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
)

// WorkerPool controls concurrent shallow checkouts and remote graph building.
type WorkerPool struct {
	concurrency int
}

func NewWorkerPool(concurrency int) *WorkerPool {
	if concurrency <= 0 {
		concurrency = 3 // default concurrency
	}
	return &WorkerPool{concurrency: concurrency}
}

// ProcessRepos clones a list of repos into a baseTempDir concurrently.
// It returns a map of repo FullName -> local checkout path.
func (wp *WorkerPool) ProcessRepos(ctx context.Context, repos []DiscoveredRepo, token, baseTempDir string) (map[string]string, error) {
	jobs := make(chan DiscoveredRepo, len(repos))
	results := make(chan struct {
		Repo DiscoveredRepo
		Path string
		Err  error
	}, len(repos))

	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < wp.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range jobs {
				targetDir := filepath.Join(baseTempDir, repo.Owner, repo.Name)
				fmt.Printf("⬇️  [MultiRepo] Cloning %s into %s\n", repo.FullName, targetDir)
				err := ShallowCheckout(ctx, repo, token, targetDir)
				results <- struct {
					Repo DiscoveredRepo
					Path string
					Err  error
				}{repo, targetDir, err}
			}
		}()
	}

	// Send jobs
	for _, r := range repos {
		jobs <- r
	}
	close(jobs)

	// Wait for completion in a background goroutine to close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	checkoutPaths := make(map[string]string)

	for res := range results {
		if res.Err != nil {
			fmt.Printf("⚠️  [MultiRepo] Failed to checkout %s: %v\n", res.Repo.FullName, res.Err)
		} else {
			fmt.Printf("✅  [MultiRepo] Checkout complete for %s\n", res.Repo.FullName)
			checkoutPaths[res.Repo.FullName] = res.Path
		}
	}

	return checkoutPaths, nil
}
