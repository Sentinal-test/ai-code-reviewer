package llm

import (
	"code-review/backend/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// Gemini 3.5 Flash — Highly capable SWE reasoning and fast model
	geminiModel = "gemini-3.5-flash"
	geminiURL   = "https://generativelanguage.googleapis.com/v1beta/models/" + geminiModel + ":generateContent"

	// GeminiFlashModel is a faster, cheaper model used for the consolidation step.
	GeminiFlashModel = "gemini-3.5-flash"
)

// FIX #4: Safe UTF-8 truncation helper
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}

	fmt.Printf("[WARNING] Context truncation triggered! Input size: %d bytes, Max allowed: %d bytes\n", len(s), maxBytes)

	// Find the last valid UTF-8 character boundary before maxBytes
	truncated := s[:maxBytes]

	// Walk backwards to drop incomplete UTF-8 characters at the boundary
	for len(truncated) > 0 {
		r, size := utf8.DecodeLastRuneInString(truncated)
		if r == utf8.RuneError && size <= 1 {
			// Incomplete rune; drop the byte and try again
			truncated = truncated[:len(truncated)-1]
		} else {
			// Valid rune at the end, boundary is clean
			break
		}
	}

	return truncated + "\n...[TRUNCATED]..."
}

// isDocumentationFile checks if a file is documentation/config and shouldn't be reviewed
func isDocumentationFile(path string) bool {
	lowerPath := strings.ToLower(path)
	baseName := strings.ToLower(filepath.Base(path))

	// Documentation file extensions
	docExtensions := []string{".md", ".txt", ".rst", ".adoc"}
	for _, ext := range docExtensions {
		if strings.HasSuffix(lowerPath, ext) {
			return true
		}
	}

	// FIX #3: More precise matching for common doc filenames
	// Use exact match or match with common doc extensions
	docFilePatterns := []string{
		"readme", "license", "changelog", "contributing",
		"code_of_conduct", "authors", "contributors",
		"history", "news", "thanks", "acknowledgments",
	}

	// Extract base name without extension for comparison
	baseWithoutExt := baseName
	for _, ext := range docExtensions {
		baseWithoutExt = strings.TrimSuffix(baseWithoutExt, ext)
	}

	// Check for exact match (e.g., "readme", "todo")
	for _, docPattern := range docFilePatterns {
		if baseWithoutExt == docPattern {
			return true
		}
	}

	// Special case: also check if filename is just the pattern (e.g., "README", "TODO", "LICENSE")
	for _, docPattern := range docFilePatterns {
		if baseName == docPattern {
			return true
		}
	}

	// Configuration/metadata files (no code logic to review)
	configFiles := []string{
		".gitignore", ".dockerignore", ".editorconfig", ".env.example",
		"makefile", ".prettierrc", ".eslintrc",
		"tsconfig.json", "package.json", "package-lock.json",
		"go.mod", "go.sum", "requirements.txt", "pipfile",
		"poetry.lock", "yarn.lock", "composer.json",
	}

	for _, configFile := range configFiles {
		if strings.Contains(lowerPath, configFile) {
			return true
		}
	}

	// New: Ignore common dependency, build, and meta directories
	ignoredDirs := []string{
		"node_modules", "vendor",
		"dist", "build", "bin", "out", "target",
		".git", ".ai-reviewer", ".idea", ".vscode",
	}

	// Check if any part of the path is an ignored directory
	// Git always uses forward slashes in diffs and paths
	pathParts := strings.Split(lowerPath, "/")
	for _, part := range pathParts {
		for _, ignored := range ignoredDirs {
			if part == ignored {
				return true
			}
		}
	}

	return false
}

// filterReviewableFiles removes documentation and config files from the map
func filterReviewableFiles(files map[string]string) map[string]string {
	filtered := make(map[string]string)
	for path, content := range files {
		if !isDocumentationFile(path) {
			filtered[path] = content
		}
	}
	return filtered
}

// FIX #1: Improved diff path extraction using regex that handles spaces and quotes
// This regex captures the two paths in 'diff --git "a/path" "b/path"' or 'diff --git a/path b/path'
var diffPathRegex = regexp.MustCompile(`^diff --git "?a/(.*?)"? "?b/(.*)"?$`)

// extractChangedLinesFromDiff extracts only the changed lines with their context
// Returns a map of file -> list of changed line sections
func extractChangedLinesFromDiff(diff string) map[string][]string {
	result := make(map[string][]string)
	lines := strings.Split(diff, "\n")

	var currentFile string
	var currentSection []string

	for _, line := range lines {
		// File header: diff --git a/file b/file
		if strings.HasPrefix(line, "diff --git") {
			if currentFile != "" && len(currentSection) > 0 {
				result[currentFile] = append(result[currentFile], strings.Join(currentSection, "\n"))
			}
			currentSection = []string{}

			// FIX #1: Use regex to extract filename (handles spaces and quoted paths)
			matches := diffPathRegex.FindStringSubmatch(line)
			if len(matches) >= 3 {
				// The regex already consumed the 'b/' prefix.
				currentFile = strings.Trim(matches[2], "\"")
			} else {
				// Fallback: try to extract using the old method for edge cases
				parts := strings.Fields(line)
				if len(parts) >= 4 {
					currentFile = strings.TrimPrefix(strings.Trim(parts[3], "\""), "b/")
				}
			}
			continue
		} else if strings.HasPrefix(line, "+++ ") && !strings.HasPrefix(line, "+++ /dev/null") {
			file := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			file = strings.TrimPrefix(file, "b/")
			if idx := strings.IndexAny(file, " \t"); idx >= 0 {
				file = file[:idx]
			}
			file = strings.Trim(file, "\"")
			if file != currentFile {
				if currentFile != "" && len(currentSection) > 0 {
					result[currentFile] = append(result[currentFile], strings.Join(currentSection, "\n"))
				}
				currentSection = []string{}
				currentFile = file
			}
			continue
		}

		// Track hunk headers and changed lines
		if strings.HasPrefix(line, "@@") {
			if len(currentSection) > 0 {
				result[currentFile] = append(result[currentFile], strings.Join(currentSection, "\n"))
			}
			currentSection = []string{line}
		} else if len(currentSection) > 0 && (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")) &&
			!strings.HasPrefix(line, "+++ ") && !strings.HasPrefix(line, "--- ") {
			currentSection = append(currentSection, line)
		} else if len(currentSection) > 0 && !strings.HasPrefix(line, "\\") {
			// Context line within a hunk
			currentSection = append(currentSection, line)
		}
	}

	// Add final section
	if currentFile != "" && len(currentSection) > 0 {
		result[currentFile] = append(result[currentFile], strings.Join(currentSection, "\n"))
	}

	return result
}

// hunkLineRegex extracts the new-file start line and count from a unified diff hunk header.
var hunkLineRegex = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// extractDiffWithContext produces a focused view of an oversized file.
// It includes: (1) the import/package header, (2) ±contextLines around each diff hunk,
// and (3) "... (lines X-Y omitted) ..." markers between windows.
func extractDiffWithContext(content string, diffSections []string, contextLines int) string {
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	if totalLines == 0 {
		return ""
	}

	// Step 1: Find the end of the import block (package + imports).
	// We always include this regardless of where changes are.
	importEnd := 0
	inImportBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") {
			importEnd = i
			continue
		}
		if strings.HasPrefix(trimmed, "import (") {
			inImportBlock = true
			continue
		}
		if inImportBlock {
			if trimmed == ")" {
				importEnd = i
				inImportBlock = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "import ") && !inImportBlock {
			importEnd = i
			continue
		}
		// Stop scanning after we've passed the header area (first non-blank, non-import line)
		if importEnd > 0 && trimmed != "" {
			break
		}
	}

	// Step 2: Parse diff sections to find which line ranges changed.
	type lineRange struct {
		start, end int // 0-indexed
	}
	var changedRanges []lineRange

	for _, section := range diffSections {
		sectionLines := strings.Split(section, "\n")
		for _, sl := range sectionLines {
			matches := hunkLineRegex.FindStringSubmatch(sl)
			if len(matches) >= 2 {
				startLine := 0
				count := 1
				fmt.Sscanf(matches[1], "%d", &startLine)
				if len(matches) >= 3 && matches[2] != "" {
					fmt.Sscanf(matches[2], "%d", &count)
				}
				// Convert to 0-indexed and add context window
				rangeStart := startLine - 1 - contextLines
				rangeEnd := startLine - 1 + count + contextLines
				if rangeStart < 0 {
					rangeStart = 0
				}
				if rangeEnd > totalLines {
					rangeEnd = totalLines
				}
				changedRanges = append(changedRanges, lineRange{rangeStart, rangeEnd})
			}
		}
	}

	// If no diff sections found, just return the import block
	if len(changedRanges) == 0 {
		var b strings.Builder
		for i := 0; i <= importEnd && i < totalLines; i++ {
			b.WriteString(fmt.Sprintf("%d: %s\n", i+1, lines[i]))
		}
		b.WriteString(fmt.Sprintf("\n... (lines %d-%d omitted — no diff hunks found) ...\n", importEnd+2, totalLines))
		return b.String()
	}

	// Step 3: Sort and merge overlapping ranges.
	sort.Slice(changedRanges, func(i, j int) bool {
		return changedRanges[i].start < changedRanges[j].start
	})

	var merged []lineRange
	current := changedRanges[0]
	for _, r := range changedRanges[1:] {
		if r.start <= current.end {
			if r.end > current.end {
				current.end = r.end
			}
		} else {
			merged = append(merged, current)
			current = r
		}
	}
	merged = append(merged, current)

	// Step 4: Build the focused output.
	var b strings.Builder

	// Always include import block first (if it's not already covered by a range)
	importCovered := false
	if len(merged) > 0 && merged[0].start <= importEnd {
		importCovered = true
	}

	if !importCovered && importEnd > 0 {
		for i := 0; i <= importEnd && i < totalLines; i++ {
			b.WriteString(fmt.Sprintf("%d: %s\n", i+1, lines[i]))
		}
		b.WriteString(fmt.Sprintf("\n... (lines %d-%d omitted) ...\n\n", importEnd+2, merged[0].start))
	}

	lastEnd := 0
	if !importCovered && importEnd > 0 {
		lastEnd = importEnd + 1
	}

	for _, r := range merged {
		if r.start > lastEnd {
			b.WriteString(fmt.Sprintf("\n... (lines %d-%d omitted) ...\n\n", lastEnd+1, r.start))
		}
		for i := r.start; i < r.end && i < totalLines; i++ {
			b.WriteString(fmt.Sprintf("%d: %s\n", i+1, lines[i]))
		}
		lastEnd = r.end
	}

	if lastEnd < totalLines {
		b.WriteString(fmt.Sprintf("\n... (lines %d-%d omitted) ...\n", lastEnd+1, totalLines))
	}

	return b.String()
}

// RunReview analyzes the diff using the provided API key, settings, and PR context.
func RunReview(ctx context.Context, provider LLMProvider, diff string, changedFiles map[string]string, dependencies map[string]string, settings models.RepoSettings, repoStructure string, prContext models.PRContext) (*models.ReviewResult, error) {
	// Filter out documentation files
	reviewableFiles := filterReviewableFiles(changedFiles)
	reviewableDeps := filterReviewableFiles(dependencies)

	if len(reviewableFiles) == 0 {
		return &models.ReviewResult{
			Summary:  "No reviewable code files changed (only documentation/config files)",
			Comments: []models.ReviewComment{},
		}, nil
	}

	// 1. Construct Prompt
	prompt := buildPrompt(diff, reviewableFiles, reviewableDeps, settings, repoStructure, prContext)

	// LOGGING: Systematic LLM Call Summary
	fmt.Println("\n" + strings.Repeat("═", 80))
	fmt.Printf("🧠 [LLM CALL] %s\n", provider.GetName())
	fmt.Println(strings.Repeat("═", 80))

	fmt.Printf("📂 Changed Files (%d):\n", len(reviewableFiles))
	for _, path := range getFileKeys(reviewableFiles) {
		fmt.Printf("  - %s\n", path)
	}

	if len(reviewableDeps) > 0 {
		fmt.Printf("\n🔍 Code Graph Context (%d files):\n", len(reviewableDeps))
		for _, path := range getFileKeys(reviewableDeps) {
			content := reviewableDeps[path]
			fmt.Printf("  + %s (%d chars)\n", path, len(content))
		}
	}

	fmt.Printf("\n📊 Meta Context:\n")
	fmt.Printf("  - Repository Structure: %d chars\n", len(repoStructure))
	fmt.Printf("  - PR Intent Context:   %d chars\n", len(buildPRContextSummary(prContext)))
	fmt.Printf("  - Total Prompt Size:   %d chars (~%d tokens)\n", len(prompt), len(prompt)/4)
	fmt.Println(strings.Repeat("═", 80))
	fmt.Println()

	// 2. Prepare Request
	req := GenerateRequest{
		SystemPrompt: "", // Built into the dynamic prompt for RunReview for now
		Messages: []Message{
			{
				Role: "user",
				Parts: []Part{
					{Text: prompt},
				},
			},
		},
		Temperature:  0.0,
		ResponseJSON: true, // we mandate a JSON array or object
	}

	// 3. Execute Request
	resp, err := provider.GenerateContent(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %v", err)
	}

	if resp.FinishReason == "EMPTY" || resp.Text == "" {
		return nil, fmt.Errorf("LLM returned empty response")
	}

	responseText := resp.Text

	// Robust parsing: try Result object first, then fallback to Array of comments
	var result models.ReviewResult
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		// Fallback: Check if it's a naked array of comments
		var comments []models.ReviewComment
		if errArray := json.Unmarshal([]byte(responseText), &comments); errArray == nil {
			result.Comments = comments
			result.Summary = "Automated review comments"
		} else {
			return nil, fmt.Errorf("failed to unmarshal JSON content: %v", err)
		}
	}

	return &result, nil
}

// formatSafeDiff rewrites a standard unified diff hunk into a semantically safe format.
// It replaces raw '-' and '+' prefixes with explicit tags to prevent the LLM's
// pattern-matching from confusing deleted lines for current, active code.
func formatSafeDiff(diffSection string) string {
	lines := strings.Split(diffSection, "\n")
	var result strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "--- ") {
			// Prefix with tag, padded to align with the other tag
			result.WriteString("[OLD_REMOVED_CODE] ")
			result.WriteString(line[1:])
			result.WriteString("\n")
		} else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++ ") {
			result.WriteString("[NEW_LIVE_CODE]    ")
			result.WriteString(line[1:])
			result.WriteString("\n")
		} else {
			// Context lines or headers (keep as is, but pad for alignment)
			if !strings.HasPrefix(line, "@@ ") && !strings.HasPrefix(line, "--- ") && !strings.HasPrefix(line, "+++ ") && len(line) > 0 && line[0] == ' ' {
				result.WriteString("                   ")
				result.WriteString(line[1:])
				result.WriteString("\n")
			} else {
				result.WriteString(line)
				result.WriteString("\n")
			}
		}
	}
	return result.String()
}

func getFileKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func buildPrompt(diff string, changedFiles map[string]string, dependencies map[string]string, settings models.RepoSettings, repoStructure string, prContext models.PRContext) string {
	// Context Window Management
	// Priority order: Changed Files (highest) > Dependencies > Repo Structure (lowest)
	// When over budget, drop the LARGEST dependency files first.
	// Budget: 720K chars ≈ 180K tokens, aligned with chunker's DefaultTokenBudget
	const MaxContextChars = 720_000

	// Helper to format changed files with FULL context + line numbers.
	formatFilesWithDiff := func(files map[string]string, diffMap map[string][]string) string {
		var b strings.Builder
		for _, path := range getFileKeys(files) {
			content := files[path]
			b.WriteString("\n═══════════════════════════════════════════════════════════════════════════════\n")
			b.WriteString(fmt.Sprintf("FILE: %s (Focused Context ±50 Lines Around Changes)\n", path))
			b.WriteString("═══════════════════════════════════════════════════════════════════════════════\n")

			// Use extractDiffWithContext to show only what matters, saving token budget
			// and reducing noise for the LLM. 50 lines is plenty for local reasoning.
			if diffSections, hasDiff := diffMap[path]; hasDiff && len(diffSections) > 0 {
				focusedContent := extractDiffWithContext(content, diffSections, 50)
				b.WriteString(focusedContent)
			} else {
				// Rare case: changed but no diff sections found? Show first 100 lines.
				lines := strings.Split(content, "\n")
				limit := 100
				if len(lines) < limit {
					limit = len(lines)
				}
				for i := 0; i < limit; i++ {
					b.WriteString(fmt.Sprintf("%d: %s\n", i+1, lines[i]))
				}
				if len(lines) > limit {
					b.WriteString(fmt.Sprintf("\n... (%d lines total, showing first %d) ...\n", len(lines), limit))
				}
			}
		}
		return b.String()
	}

	// Extract changed lines from diff
	diffMap := extractChangedLinesFromDiff(diff)

	// Build annotated changed files section (always included in full — highest priority)
	currentSize := 0
	changedContent := formatFilesWithDiff(changedFiles, diffMap)
	currentSize += len(changedContent)

	// Priority-based dependency inclusion:
	// Include deps by ascending size (smallest first). When budget is exhausted,
	// drop remaining deps instead of cutting mid-file.
	type depEntry struct {
		Path    string
		Content string
		Size    int
	}

	depKeys := getFileKeys(dependencies)
	depEntries := make([]depEntry, 0, len(depKeys))
	for _, path := range depKeys {
		formatted := fmt.Sprintf("\n--- FILE: %s ---\n%s\n", path, dependencies[path])
		depEntries = append(depEntries, depEntry{Path: path, Content: formatted, Size: len(formatted)})
	}

	// Sort by size ascending — smallest (most critical / compact) deps survive
	sort.Slice(depEntries, func(i, j int) bool {
		return depEntries[i].Size < depEntries[j].Size
	})

	var depsBuilder strings.Builder
	var droppedDeps []string
	for _, dep := range depEntries {
		if currentSize+dep.Size > MaxContextChars {
			droppedDeps = append(droppedDeps, dep.Path)
			continue
		}
		depsBuilder.WriteString(dep.Content)
		currentSize += dep.Size
	}
	depsContent := depsBuilder.String()

	if len(droppedDeps) > 0 {
		fmt.Printf("⚠️  [Context Budget] Dropped %d large dependency files to fit within %dK token limit:\n", len(droppedDeps), MaxContextChars/4/1000)
		for _, d := range droppedDeps {
			fmt.Printf("    - %s\n", d)
		}
	}

	// Repo structure (lowest priority — truncate or omit if needed)
	if currentSize+len(repoStructure) > MaxContextChars {
		available := MaxContextChars - currentSize
		if available > 1000 { // Only include if there's meaningful space
			repoStructure = truncateUTF8(repoStructure, available)
			fmt.Printf("⚠️  [Context Budget] Repo structure truncated to %d chars\n", available)
		} else {
			repoStructure = ""
			fmt.Println("⚠️  [Context Budget] Repo structure omitted (no space)")
		}
	}

	var layers = []string{}
	if settings.SecurityEnabled {
		layers = append(layers, "Security (vulnerabilities, secrets, unsafe operations)")
	}
	if settings.BugEnabled {
		layers = append(layers, "Bugs (logic errors, crashes, data corruption)")
	}
	if settings.LintEnabled {
		layers = append(layers, "Lint (style, formatting, best practices)")
	}
	if settings.PerformanceEnabled {
		layers = append(layers, "Performance (inefficiencies, memory leaks, algorithmic issues)")
	}
	if settings.ArchitectureEnabled {
		layers = append(layers, "Architecture (design patterns, structure, maintainability)")
	}

	// Build PR context section with clear intent guidance
	var prContextSection string
	if prContext.Title != "" || prContext.Body != "" || len(prContext.CommitMessages) > 0 {
		prContextSection = `
═══════════════════════════════════════════════════════════════════════════════
PR CONTEXT (Developer Intent - Use as Background Only)
═══════════════════════════════════════════════════════════════════════════════
This section provides the developer's stated intent. Your review must be based on
the ACTUAL CODE CHANGES, not on whether the code matches the stated intent.

**IMPORTANT DISTINCTIONS:**
- If developer says "added logging for debugging" → This is INTENT, not a defect
- If the logging code actually exposes sensitive data → This IS a defect (review it)
- If developer says "fixed bug X" but code still has bug Y → Review bug Y
- If developer says "temporary change" but code has security flaw → Review the flaw

**USE THIS CONTEXT TO:**
✓ Understand what the developer was trying to accomplish
✓ Distinguish between intentional debugging code vs accidental security issues
✓ Recognize temporary/experimental code (but still flag genuine issues in it)
✓ Understand business logic context for the changes

**DO NOT USE THIS CONTEXT TO:**
✗ Assume code is correct because it matches the description
✗ Skip reviewing code that's marked as "debug", "temp", or "testing"
✗ Flag intentional debugging/logging as issues UNLESS it has actual security implications
✗ Treat the PR description as source of truth for correctness

`
		if prContext.Title != "" {
			prContextSection += fmt.Sprintf("**PR Title:** %s\n", prContext.Title)
		}
		if prContext.Body != "" {
			body := prContext.Body
			if len(body) > 1000 {
				body = truncateUTF8(body, 1000)
			}
			prContextSection += fmt.Sprintf("**PR Description:** %s\n", body)
		}
		if len(prContext.CommitMessages) > 0 {
			prContextSection += "\n**Commit Messages (last 10):**\n"
			msgs := prContext.CommitMessages
			if len(msgs) > 10 {
				msgs = msgs[len(msgs)-10:]
			}
			for _, msg := range msgs {
				prContextSection += fmt.Sprintf("  • %s\n", msg)
			}
		}
		prContextSection += "\n"
	}

	return fmt.Sprintf(`You are an automated code defect detection system performing static analysis on code changes.
Your SOLE objective is to identify defects, vulnerabilities, bugs, and code quality issues.

%s

═══════════════════════════════════════════════════════════════════════════════
PRIMARY ANALYSIS TARGET: Changed Code Files
═══════════════════════════════════════════════════════════════════════════════
Below are the changed files. You are 
provided with a FOCUSED VIEW: the package/import header and ±50 lines of context 
around each change hunk.

If you need more context to understand a type, function, or caller that is outside 
this window, YOU MUST USE THE PROVIDED TOOLS (get_symbol_definition, get_file_content, etc.).

The diff hunks show exactly what was added (+) and removed (-).

%s

═══════════════════════════════════════════════════════════════════════════════
SUPPORTING CONTEXT: Code Graph (Relationships + Behavioral Summaries)
═══════════════════════════════════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════════════════════════════════
SUPPORTING CONTEXT: Repository Structure (For Architecture Analysis)
═══════════════════════════════════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════════════════════════════════
ACTIVE DETECTION LAYERS
═══════════════════════════════════════════════════════════════════════════════
%v

═══════════════════════════════════════════════════════════════════════════════
MANDATORY ANALYSIS RULES - STRICT COMPLIANCE REQUIRED
═══════════════════════════════════════════════════════════════════════════════

RULE 1 - FOCUS ON ACTUAL CODE CHANGES:
  ✓ Analyze ONLY the code within "Change Block" sections (marked in comments above)
  ✓ Each comment MUST reference a specific line number from the '+' (added/modified) lines in the diff
  ✓ If you identify a general problem in a block, tag it on the first relevant '+' line of that block
  ✓ Use the complete file content to understand context, but flag issues only in changes
  ✗ NEVER comment on unchanged code or line numbers outside the provided change blocks
  ✗ NEVER placeholder line numbers like 0 or 1 unless it is a file-level architectural issue

RULE 2 - CRITICAL ISSUES IN CONTEXT:
  ✓ IF you find a CRITICAL security vulnerability or severe bug in dependency/context files
  ✓ AND it DIRECTLY impacts or is called by the changed code
  ✓ THEN report it with: file=<dependency_file>, line=0, message="[CONTEXT] <problem>"
  ✓ Use ONLY for severity: critical or warning
  ✗ DO NOT use for general suggestions or info-level issues

  MULTI-REPO NOTE: In a microservices or multi-repo architecture, dependency files may
  come from OTHER repositories. If you see file paths prefixed with a repo name or from
  a clearly different service, treat these as cross-service dependencies. Look for:
  - API contract mismatches between the changed code and cross-repo consumers/producers
  - Shared model or type definition changes that could break deserialization in other services
  - Event/message schema changes without backward compatibility
  - Authentication/authorization flow changes that affect other services
  Flag concrete cross-repo breakage as severity: warning or critical.

RULE 3 - DISTINGUISH INTENT FROM DEFECTS:
  ✓ Read the PR Context to understand what the developer intended to do
  ✓ If code has "debug", "temp", or "test" comments, recognize these as intentional
  ✓ BUT still flag genuine security/correctness issues in that code
  
  Examples:
  • Code: "log.Debug(response)" + PR says "added debug logging"
    → Do NOT flag as security issue (it's intentional debugging)
  
  • Code: "log.Info(user.Password)" + PR says "improved logging"
    → DO flag as critical security issue (exposing sensitive data is always wrong)
  
  • Code: "// TODO: add auth" + PR says "temporary endpoint for testing"
    → DO flag missing authentication (being temporary doesn't make it safe)
  
  • Code: "time.Sleep(5 * time.Second)" + PR says "debugging race condition"
    → Do NOT flag (it's intentional debug code, not a production bug)

RULE 4 - DOCUMENTATION FILES EXCLUSION:
  ✗ DO NOT review documentation files (README, CHANGELOG, .md files, etc.)
  ✗ DO NOT review configuration files (package.json, go.mod, .gitignore, etc.)
  ✓ These files have been pre-filtered and should not appear in the changed files
  ✓ Focus only on actual code logic that can have defects

RULE 5 - ZERO FALSE POSITIVES:
  ✗ DO NOT comment on code that is correct, functional, or follows best practices
  ✗ DO NOT provide praise, confirmations, or acknowledgments
  ✗ DO NOT comment on style preferences unless they cause bugs or violate language standards
  ✗ DO NOT provide educational content or alternative implementations
  ✗ DO NOT comment if you cannot identify a concrete, measurable defect
  ✓ Silence on correct code is EXPECTED and DESIRED

RULE 6 - COMMENT STRUCTURE (Strict Format):
  Format: "<Problem>. <Fix>."
  
  ✓ CORRECT Examples:
    - "SQL injection via unsanitized input. Use parameterized queries."
    - "Nil pointer dereference if user is nil. Add nil check before user.ID access."
    - "Race condition on shared map access. Protect with mutex or use sync.Map."
    - "Memory leak from unclosed file handle. Add defer file.Close() after error check."
    - "Password logged in plain text. Remove sensitive data from logs or redact it."
  
  ✗ INCORRECT Examples:
    - "This is good code" (praise - forbidden)
    - "Logging full response might be risky" (vague - not specific enough)
    - "Consider using a different pattern" (suggestion without concrete problem)
    - "This could be optimized" (no measurable defect identified)
  
  Maximum: 2 concise sentences per comment
  
RULE 7 - SEVERITY CLASSIFICATION (Precise Definitions):
  critical: Security vulnerabilities (SQL injection, XSS, auth bypass, exposed credentials/tokens),
            data corruption, guaranteed crashes/panics, breaking API changes that cause failures
  
  warning:  Logic errors, potential nil panics, resource leaks (unclosed files/connections),
            race conditions, incorrect error handling, improper error propagation, off-by-one errors,
            unsafe type assertions, missing important validations
  
  info:     Minor inefficiencies, missing non-critical error checks, suboptimal patterns that
            don't affect correctness, redundant code, missing comments on complex logic

RULE 8 - CONTEXT SENSITIVITY:
  When analyzing issues, consider:
  
  ✓ Is this debug/test code explicitly mentioned in PR context?
    → If yes, be more lenient with logging, sleeps, hardcoded values
    → BUT still flag actual security issues (exposed secrets, missing auth, etc.)
  
  ✓ Is this a temporary workaround mentioned in commits?
    → Flag the underlying issue but acknowledge temporary nature
    → Example: "Missing input validation. Add before production deployment."
  
  ✓ Is the "issue" actually the intended behavior?
    → Check PR title/description first before flagging
    → Example: PR says "added verbose logging for debugging" → don't flag verbose logs
  
  ✗ Never use PR context as excuse to ignore genuine defects
    → Intentional doesn't mean correct
    → Debug code can still have security flaws worth fixing

RULE 9 - OUTPUT FORMAT (Strict JSON Schema):
  ✓ MUST be valid JSON with no markdown formatting
  ✓ Summary format: "Found X critical, Y warning, Z info issue(s)" (use exact counts)
  ✓ If no issues: {"summary": "No issues detected", "comments": []}
  ✗ NEVER include markdown code fences, extra text, or explanations

═══════════════════════════════════════════════════════════════════════════════
OUTPUT JSON SCHEMA (No markdown, no extra text)
═══════════════════════════════════════════════════════════════════════════════
{
  "summary": "Found <count> critical, <count> warning, <count> info issue(s)" | "No issues detected",
  "comments": [
    {
      "file": "relative/path/to/file.go",
      "line": 42,
      "layer": "security|bug|lint|performance|architecture",
      "message": "<Problem>. <Fix>.",
      "severity": "critical|warning|info"
    }
  ]
}

═══════════════════════════════════════════════════════════════════════════════
ANALYSIS STRATEGY
═══════════════════════════════════════════════════════════════════════════════

1. Read PR Context first to understand developer intent
2. Identify all Change Blocks in the files
3. For each change:
   a. Check if it's intentional debug/test code from PR context
   b. Analyze for security vulnerabilities (always flag these)
   c. Check for logic errors and bugs
   d. Verify error handling and edge cases
   e. Look for resource leaks and race conditions
4. Use complete file content to verify:
   - Type compatibility
   - Function signatures
   - Imported dependencies
   - Surrounding context
5. Cross-reference with dependency files for interface validation
6. When dependency files come from OTHER repositories (multi-repo/microservices setup),
   verify that the changed code's API contracts, data models, event schemas, and auth
   flows remain compatible with those external consumers and producers
7. Generate comments only for genuine defects

REMEMBER: You are a DEFECT DETECTOR with context awareness.
- Understand developer intent, but review the actual code
- Flag real problems, not intentional debugging code
- Use severity appropriately based on actual risk
- Silence on correct code is correct behavior

BEGIN ANALYSIS NOW.
`, prContextSection, changedContent, depsContent, repoStructure, layers)
}

func buildPRContextSummary(prContext models.PRContext) string {
	var b strings.Builder
	if prContext.Title != "" {
		b.WriteString(prContext.Title)
	}
	if prContext.Body != "" {
		b.WriteString(prContext.Body)
	}
	for _, msg := range prContext.CommitMessages {
		b.WriteString(msg)
	}
	return b.String()
}

// ConsolidateResults merges results from multiple chunks into a single ReviewResult.
// Deduplicates comments by (file, line, message) to avoid repeats across overlapping context.
func ConsolidateResults(results []*models.ReviewResult) *models.ReviewResult {
	if len(results) == 0 {
		return &models.ReviewResult{
			Summary:  "No issues detected",
			Comments: []models.ReviewComment{},
		}
	}
	if len(results) == 1 {
		results[0].Resolutions = models.MergeResolutions(results[0].Resolutions)
		return results[0]
	}

	// Deduplicate by (file, line, truncated message)
	type commentKey struct {
		File    string
		Line    int
		MsgHash string
	}

	seen := make(map[commentKey]bool)
	var allComments []models.ReviewComment
	var summaries []string
	var allResolutions []models.Resolution

	for _, r := range results {
		if r == nil {
			continue
		}
		if r.Summary != "" {
			summaries = append(summaries, r.Summary)
		}
		for _, c := range r.Comments {
			// Use first 80 chars of message as hash to catch near-dupes
			msgHash := c.Message
			if len(msgHash) > 80 {
				msgHash = msgHash[:80]
			}
			key := commentKey{File: c.File, Line: c.Line, MsgHash: msgHash}
			if !seen[key] {
				seen[key] = true
				allComments = append(allComments, c)
			}
		}

		allResolutions = append(allResolutions, r.Resolutions...)
	}

	// Build consolidated summary
	critCount, warnCount, infoCount := 0, 0, 0
	for _, c := range allComments {
		switch c.Severity {
		case "critical":
			critCount++
		case "warning":
			warnCount++
		case "info":
			infoCount++
		}
	}

	summary := fmt.Sprintf("Found %d critical, %d warning, %d info issue(s) across %d chunks",
		critCount, warnCount, infoCount, len(results))
	if len(allComments) == 0 {
		summary = "No issues detected"
	}

	fmt.Printf("\n🔄 [Consolidation] %d chunks → %d unique comments (deduped from %d total)\n",
		len(results), len(allComments), countTotalComments(results))

	return &models.ReviewResult{
		Summary:     summary,
		Comments:    allComments,
		Resolutions: models.MergeResolutions(allResolutions),
	}
}

func countTotalComments(results []*models.ReviewResult) int {
	total := 0
	for _, r := range results {
		if r != nil {
			total += len(r.Comments)
		}
	}
	return total
}
