package memory

import (
	"code-review/backend/internal/models"
	"testing"
)

func TestFingerprint_Deterministic(t *testing.T) {
	hash1 := Fingerprint("main.go", "bug", "This is a test message. ")
	hash2 := Fingerprint("main.go", "bug", "this is a  test message.")
	hash3 := Fingerprint("main.go", "bug", "This\nis a test message.")

	if hash1 != hash2 || hash2 != hash3 {
		t.Fatalf("Fingerprints should match for variations in whitespace/case. Got:\n%s\n%s\n%s", hash1, hash2, hash3)
	}
}

func TestFingerprint_DifferentInputs(t *testing.T) {
	hash1 := Fingerprint("main.go", "bug", "Message A")
	hash2 := Fingerprint("main.go", "security", "Message A") // diff layer
	hash3 := Fingerprint("utils.go", "bug", "Message A")     // diff file
	hash4 := Fingerprint("main.go", "bug", "Message B")      // diff message

	if hash1 == hash2 || hash1 == hash3 || hash1 == hash4 {
		t.Fatalf("Expected all hashes to be different")
	}
}

func TestParseMarker(t *testing.T) {
	body := "Some introductory text\n<!-- ai-reviewer:v1 file=utils.go line=42 severity=high layer=security hash=abcdef123456 -->\n\n**[HIGH]** Security\n\nMessage here"
	pf := ParseMarker(body, "other.go", 99, 123)

	if pf == nil {
		t.Fatalf("Failed to parse valid marker")
	}

	if pf.File != "utils.go" || pf.Line != 42 || pf.Severity != "high" || pf.Layer != "security" || pf.Hash != "abcdef123456" || pf.CommentID != 123 {
		t.Fatalf("Parsed incorrectly: %+v", pf)
	}
}

func TestParseMarker_NoMarker(t *testing.T) {
	body := "Just a regular human comment"
	if ParseMarker(body, "main.go", 1, 1) != nil {
		t.Fatalf("Should return nil for body without marker")
	}
}

func TestDeduplicate(t *testing.T) {
	prev := []PreviousFinding{
		{
			File:     "auth.go",
			Line:     50,
			Severity: "high",
			Layer:    "security",
			Hash:     Fingerprint("auth.go", "security", "Missing token validation"),
		},
		{
			File:     "utils.go",
			Line:     100,
			Severity: "medium",
			Layer:    "bug",
			Hash:     Fingerprint("utils.go", "bug", "NPE possible here"),
		},
	}

	newFindings := []models.ReviewComment{
		// Duplicate: exact fingerprint match, line drifted by +2
		{
			File:     "auth.go",
			Line:     52,
			Severity: "high",
			Layer:    "security",
			Message:  "missing token validation", // case/spacing differs, still same hash
		},
		// Duplicate: different fingerprint (LLM reworded), but same file, layer, and close line (±3)
		{
			File:     "utils.go",
			Line:     102,
			Severity: "medium",
			Layer:    "bug",
			Message:  "A null pointer exception might occur if req is nil",
		},
		// New finding: exact fingerprint, but line shifted by 20 (too far)
		{
			File:     "auth.go",
			Line:     80,
			Severity: "high",
			Layer:    "security",
			Message:  "missing token validation",
		},
		// Completely new finding
		{
			File:     "main.go",
			Line:     10,
			Severity: "low",
			Layer:    "structure",
			Message:  "Consider moving this",
		},
	}

	toPost, skipped := Deduplicate(newFindings, prev)

	if skipped != 2 {
		t.Fatalf("Expected to skip 2 duplicates, skipped %d", skipped)
	}
	if len(toPost) != 2 {
		t.Fatalf("Expected 2 findings to post, got %d", len(toPost))
	}

	if toPost[0].File != "auth.go" || toPost[0].Line != 80 {
		t.Errorf("Expected auth.go:80 to be posted, got %s:%d", toPost[0].File, toPost[0].Line)
	}
	if toPost[1].File != "main.go" {
		t.Errorf("Expected main.go to be posted, got %s", toPost[1].File)
	}

	// Verify the hash of the first finding allows us to verify whitespace stripping works
	expectedHash := Fingerprint("auth.go", "security", "Missing token validation")
	actualHash := Fingerprint(newFindings[0].File, newFindings[0].Layer, newFindings[0].Message)
	if expectedHash != actualHash {
		t.Errorf("Hashes do not match despite same string when normalized. expected=%v actual=%v", expectedHash, actualHash)
	}
}

func TestDeduplicate_FallbackMatches(t *testing.T) {
	prev := []PreviousFinding{
		{
			File:     "handlers.go",
			Line:     200,
			Severity: "high",
			Layer:    "security",
			Hash:     "fakehash1",
		},
	}

	tests := []struct {
		name     string
		finding  models.ReviewComment
		isDup    bool
	}{
		{
			name: "Same file and layer, line +2 vs prev -> dup",
			finding: models.ReviewComment{File: "handlers.go", Line: 202, Layer: "security", Message: "Something insecure"},
			isDup: true,
		},
		{
			name: "Same file and layer, line +5 vs prev -> not dup (too far for fallback)",
			finding: models.ReviewComment{File: "handlers.go", Line: 205, Layer: "security", Message: "Something insecure"},
			isDup: false,
		},
		{
			name: "Same file and line, different layer -> not dup",
			finding: models.ReviewComment{File: "handlers.go", Line: 200, Layer: "bug", Message: "Something buggy"},
			isDup: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toPost, skipped := Deduplicate([]models.ReviewComment{tt.finding}, prev)
			if tt.isDup {
				if skipped != 1 || len(toPost) != 0 {
					t.Errorf("Expected to skip duplicate, got skipped=%d len(toPost)=%d", skipped, len(toPost))
				}
			} else {
				if skipped != 0 || len(toPost) != 1 {
					t.Errorf("Expected to post finding, got skipped=%d len(toPost)=%d", skipped, len(toPost))
				}
			}
		})
	}
}
