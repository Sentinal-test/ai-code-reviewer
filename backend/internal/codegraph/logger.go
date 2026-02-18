package codegraph

import (
	"fmt"
	"time"
)

// BuildStats tracks what the code graph built during an operation.
type BuildStats struct {
	Mode         string        // "full_build" | "delta_update" | "cache_hit" | "analysis"
	TotalFiles   int           // total files in scope
	ParsedFiles  int           // files actually parsed with tree-sitter
	SkippedFiles int           // files skipped (unsupported extension)
	Definitions  int           // definitions found
	References   int           // references extracted
	Edges        int           // cross-file edges
	CacheStatus  string        // "hit" | "miss" | "stale" | ""
	DeltaFiles   int           // files re-parsed in delta mode
	Duration     time.Duration // total operation time
}

// Print outputs a structured summary of the code graph build.
func (s *BuildStats) Print() {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("📊 [Code Graph] Build Summary")
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Printf("  Mode:           %s\n", s.Mode)
	if s.CacheStatus != "" {
		fmt.Printf("  Cache:          %s\n", s.CacheStatus)
	}
	if s.DeltaFiles > 0 {
		fmt.Printf("  Delta files:    %d re-parsed (out of %d total)\n", s.DeltaFiles, s.TotalFiles)
	} else {
		fmt.Printf("  Files parsed:   %d (%d skipped)\n", s.ParsedFiles, s.SkippedFiles)
	}
	fmt.Printf("  Definitions:    %d\n", s.Definitions)
	fmt.Printf("  References:     %d\n", s.References)
	fmt.Printf("  Edges:          %d\n", s.Edges)
	fmt.Printf("  Duration:       %s\n", s.Duration.Round(time.Millisecond))
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()
}
