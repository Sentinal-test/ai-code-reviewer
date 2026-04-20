package orchestrator

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// FileCoverageStatus represents what happened to a file during the review process.
type FileCoverageStatus string

const (
	CoverageStatusReviewed        FileCoverageStatus = "reviewed"
	CoverageStatusSkippedDocConf  FileCoverageStatus = "skipped_doc_config"
	CoverageStatusSkippedDevRules FileCoverageStatus = "skipped_dev_rules"
	CoverageStatusPartialOversize FileCoverageStatus = "partial_oversized"
	CoverageStatusError           FileCoverageStatus = "chunk_error"
	CoverageStatusPending         FileCoverageStatus = "pending"
)

// FileStatus tracks the review coverage for a single file.
type FileStatus struct {
	Path   string
	Status FileCoverageStatus
	Reason string // Optional context (e.g., "Ignored by focus rules", or error message)
}

// CoverageManifest tracks the review status of all changed files in a PR.
// It provides an audit trail answering "which hunks were actually reviewed?".
type CoverageManifest struct {
	mu    sync.RWMutex
	Files map[string]*FileStatus
}

// NewCoverageManifest initializes a new empty manifest.
func NewCoverageManifest() *CoverageManifest {
	return &CoverageManifest{
		Files: make(map[string]*FileStatus),
	}
}

// AddFile registers a file with an initial status.
func (m *CoverageManifest) AddFile(path string, status FileCoverageStatus, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Files[path] = &FileStatus{
		Path:   path,
		Status: status,
		Reason: reason,
	}
}

// UpdateStatus changes the status of a previously registered file.
func (m *CoverageManifest) UpdateStatus(path string, status FileCoverageStatus, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fs, exists := m.Files[path]; exists {
		fs.Status = status
		fs.Reason = reason
	} else {
		m.Files[path] = &FileStatus{
			Path:   path,
			Status: status,
			Reason: reason,
		}
	}
}

// MarkFiles updates a batch of files with the same status.
func (m *CoverageManifest) MarkFiles(paths []string, status FileCoverageStatus, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range paths {
		if fs, exists := m.Files[p]; exists {
			fs.Status = status
			fs.Reason = reason
			continue
		}
		m.Files[p] = &FileStatus{
			Path:   p,
			Status: status,
			Reason: reason,
		}
	}
}

// GenerateSummary produces a Markdown-formatted coverage string for the PR comment.
func (m *CoverageManifest) GenerateSummary() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.Files)
	if total == 0 {
		return "📊 **Coverage:** No files processed."
	}

	reviewed := 0
	skipped := 0
	partial := 0
	errors := 0
	pending := 0

	var skippedDetails []string

	paths := make([]string, 0, total)
	for p := range m.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		fs := m.Files[p]
		switch fs.Status {
		case CoverageStatusReviewed:
			reviewed++
		case CoverageStatusPartialOversize:
			partial++
			reviewed++ // Count partial as at least partially reviewed
		case CoverageStatusSkippedDocConf, CoverageStatusSkippedDevRules:
			skipped++
			reason := "doc/config"
			if fs.Status == CoverageStatusSkippedDevRules {
				reason = "ignore rules"
			}
			skippedDetails = append(skippedDetails, fmt.Sprintf("`%s` (%s)", p, reason))
		case CoverageStatusError:
			errors++
			skippedDetails = append(skippedDetails, fmt.Sprintf("`%s` (error)", p))
		case CoverageStatusPending:
			pending++
			skippedDetails = append(skippedDetails, fmt.Sprintf("`%s` (pending/failed)", p))
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("📊 **Coverage:** %d/%d files reviewed", reviewed, total))

	if skipped > 0 || errors > 0 || pending > 0 {
		b.WriteString(" (")
		parts := []string{}
		if skipped > 0 {
			parts = append(parts, fmt.Sprintf("%d skipped", skipped))
		}
		if errors > 0 {
			parts = append(parts, fmt.Sprintf("%d failed", errors))
		}
		if pending > 0 {
			parts = append(parts, fmt.Sprintf("%d pending/incomplete", pending))
		}
		b.WriteString(strings.Join(parts, ", "))
		b.WriteString(")")
	}

	if len(skippedDetails) > 0 {
		// Only list up to 5 skipped files to avoid noise, summarize the rest
		limit := 5
		if len(skippedDetails) <= limit+2 {
			// If it's just 6 or 7, list them all rather than truncating 2
			limit = len(skippedDetails)
		}

		b.WriteString("  \n_Skipped:_ ")
		if len(skippedDetails) > limit {
			b.WriteString(strings.Join(skippedDetails[:limit], ", "))
			b.WriteString(fmt.Sprintf(", and %d more.", len(skippedDetails)-limit))
		} else {
			b.WriteString(strings.Join(skippedDetails, ", "))
		}
	}

	return b.String()
}
