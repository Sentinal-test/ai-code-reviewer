package llm

import (
	"bytes"
	"code-review/backend/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// FIX #2: Use valid Gemini model name
	geminiModel = "gemini-2.5-flash" // Changed from "gemini-3-flash-preview"
	geminiURL   = "https://generativelanguage.googleapis.com/v1beta/models/" + geminiModel + ":generateContent"
)

// FIX #4: Safe UTF-8 truncation helper
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	
	// Find the last valid UTF-8 character boundary before maxBytes
	truncated := s[:maxBytes]
	
	// Walk backwards to find a valid UTF-8 boundary
	for len(truncated) > 0 {
		if utf8.ValidString(truncated) {
			return truncated + "\n...[TRUNCATED]..."
		}
		// Remove one byte and try again
		truncated = truncated[:len(truncated)-1]
	}
	
	return "\n...[TRUNCATED]..."
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

// FIX #1: Improved diff path extraction using regex
var diffPathRegex = regexp.MustCompile(`^diff --git a/(.*) b/(.*)$`)

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
				// Use b/ path (destination), remove quotes if present
				currentFile = strings.Trim(matches[2], "\"")
			} else {
				// Fallback: try to extract using the old method for edge cases
				parts := strings.Fields(line)
				if len(parts) >= 4 {
					currentFile = strings.TrimPrefix(parts[3], "b/")
					currentFile = strings.Trim(currentFile, "\"")
				}
			}
			continue
		}
		
		// Track hunk headers and changed lines
		if strings.HasPrefix(line, "@@") {
			if len(currentSection) > 0 {
				result[currentFile] = append(result[currentFile], strings.Join(currentSection, "\n"))
			}
			currentSection = []string{line}
		} else if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
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

// RunReview analyzes the diff using the provided API key, settings, and PR context.
func RunReview(ctx context.Context, client *http.Client, diff string, changedFiles map[string]string, dependencies map[string]string, settings models.RepoSettings, repoStructure string, apiKey string, prContext models.PRContext) (*models.ReviewResult, error) {
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

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("🔍 [REVIEW PASS] - PROMPT CONTEXT")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("Diff Size: %d bytes\n", len(diff))
	fmt.Printf("Reviewable Files: %v\n", getFileKeys(reviewableFiles))
	fmt.Printf("Dependency Files: %v\n", getFileKeys(reviewableDeps))
	fmt.Printf("Filtered Out (docs/config): %d files\n", len(changedFiles)-len(reviewableFiles))

	// 2. Prepare Request
	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"responseMimeType": "application/json",
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	url := fmt.Sprintf("%s?key=%s", geminiURL, apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 3. Execute Request
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(body))
	}

	// 4. Parse Response
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %v", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("LLM returned empty response")
	}

	responseText := geminiResp.Candidates[0].Content.Parts[0].Text

	// Robust parsing: try Result object first, then fallback to Array of comments
	var result models.ReviewResult
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		// Fallback: Check if it's a naked array of comments
		var comments []models.ReviewComment
		if errArray := json.Unmarshal([]byte(responseText), &comments); errArray == nil {
			result.Comments = comments
			result.Summary = "Automated review comments"
		} else {
			return nil, fmt.Errorf("failed to unmarshal JSON content: %v | content: %s", err, responseText)
		}
	}

	return &result, nil
}

func getFileKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func buildPrompt(diff string, changedFiles map[string]string, dependencies map[string]string, settings models.RepoSettings, repoStructure string, prContext models.PRContext) string {
	// Context Window Management
	// Priority: Complete Files with Diff Annotations > Dependencies > Repo Structure
	// Target Max Chars: ~400,000 (approx 100k tokens safety)
	const MaxContextChars = 400000

	// Helper to format files with inline diff annotations
	formatFilesWithDiff := func(files map[string]string, diffMap map[string][]string) string {
		var b strings.Builder
		for path, content := range files {
			b.WriteString(fmt.Sprintf("\n--- FILE: %s ---\n", path))
			
			// If we have diff information for this file, annotate it
			if diffSections, hasDiff := diffMap[path]; hasDiff && len(diffSections) > 0 {
				b.WriteString("/* CHANGED SECTIONS IN THIS FILE: */\n")
				for i, section := range diffSections {
					b.WriteString(fmt.Sprintf("/* Change Block %d:\n%s\n*/\n\n", i+1, section))
				}
				b.WriteString("/* COMPLETE FILE CONTENT FOR CONTEXT: */\n")
			}
			
			b.WriteString(content)
			b.WriteString("\n")
		}
		return b.String()
	}

	// Helper for dependencies (no diff annotations)
	formatFiles := func(files map[string]string) string {
		var b strings.Builder
		for path, content := range files {
			b.WriteString(fmt.Sprintf("\n--- FILE: %s ---\n%s\n", path, content))
		}
		return b.String()
	}

	// Extract changed lines from diff
	diffMap := extractChangedLinesFromDiff(diff)

	// Build annotated changed files section
	currentSize := 0
	changedContent := formatFilesWithDiff(changedFiles, diffMap)
	
	// FIX #4: Use UTF-8 safe truncation
	if len(changedContent) > MaxContextChars {
		changedContent = truncateUTF8(changedContent, MaxContextChars)
	}
	currentSize += len(changedContent)

	// Add dependencies
	depsContent := formatFiles(dependencies)
	if currentSize+len(depsContent) > MaxContextChars {
		available := MaxContextChars - currentSize
		if available > 0 {
			if len(depsContent) > available {
				depsContent = truncateUTF8(depsContent, available)
			}
		} else {
			depsContent = ""
		}
	}
	currentSize += len(depsContent)

	// Repo structure
	if currentSize+len(repoStructure) > MaxContextChars {
		available := MaxContextChars - currentSize
		if available > 0 {
			if len(repoStructure) > available {
				repoStructure = truncateUTF8(repoStructure, available)
			}
		} else {
			repoStructure = ""
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
Below are the complete files that were modified, with inline annotations showing 
exactly what changed. Focus your analysis on the CHANGED SECTIONS marked in comments.

The complete file content is provided to help you understand:
- The context in which changes were made
- How changes interact with existing code
- Whether changes break existing functionality
- Type signatures, function definitions, and dependencies

%s

═══════════════════════════════════════════════════════════════════════════════
SUPPORTING CONTEXT: Related Dependencies (For Interface/Type Verification)
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
  ✓ Each comment MUST reference a specific line number from the changed code
  ✓ Use the complete file content to understand context, but flag issues only in changes
  ✗ NEVER comment on unchanged code unless Rule 2 applies

RULE 2 - CRITICAL ISSUES IN CONTEXT:
  ✓ IF you find a CRITICAL security vulnerability or severe bug in dependency/context files
  ✓ AND it DIRECTLY impacts or is called by the changed code
  ✓ THEN report it with: file=<dependency_file>, line=0, message="[CONTEXT] <problem>"
  ✓ Use ONLY for severity: critical or warning
  ✗ DO NOT use for general suggestions or info-level issues

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
6. Generate comments only for genuine defects

REMEMBER: You are a DEFECT DETECTOR with context awareness.
- Understand developer intent, but review the actual code
- Flag real problems, not intentional debugging code
- Use severity appropriately based on actual risk
- Silence on correct code is correct behavior

BEGIN ANALYSIS NOW.
`, prContextSection, changedContent, depsContent, repoStructure, layers)
}

// AnalyzeDependencyNeeds asks the LLM which other files are needed for context.
func AnalyzeDependencyNeeds(ctx context.Context, client *http.Client, diff string, changedFiles map[string]string, repoStructure string, prContext models.PRContext, apiKey string) ([]string, error) {
	// Filter out documentation files from changed files list
	reviewableFiles := filterReviewableFiles(changedFiles)
	
	var fileList []string
	for path := range reviewableFiles {
		fileList = append(fileList, path)
	}

	// If no code files changed, no dependencies needed
	if len(fileList) == 0 {
		return []string{}, nil
	}

	prompt := fmt.Sprintf(`You are a dependency analyzer for automated code review systems.
Your task is to identify which additional repository files are REQUIRED to accurately validate the changes in the provided diff.

═══════════════════════════════════════════════════════════════════════════════
INPUT: GIT DIFF (Code Changes Only)
═══════════════════════════════════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════════════════════════════════
ALREADY AVAILABLE: Changed Code Files
═══════════════════════════════════════════════════════════════════════════════
%v

Note: Documentation files (.md, README, etc.) and config files have been filtered out.
Only actual code files are being reviewed.

═══════════════════════════════════════════════════════════════════════════════
AVAILABLE: Repository Structure
═══════════════════════════════════════════════════════════════════════════════
%s

═══════════════════════════════════════════════════════════════════════════════
ANALYSIS TASK
═══════════════════════════════════════════════════════════════════════════════

Analyze the diff and identify CODE files that are CRITICAL for validating the changes.

INCLUDE files that provide:
✓ Type definitions, struct definitions, or interfaces used in the changed code
✓ Function/method signatures that are called by the changes
✓ Constants, enums, or configuration referenced in the code
✓ Parent classes, base implementations, or mixins that changed code extends
✓ Database models or schema if the diff includes queries/database operations
✓ API route definitions if the diff implements handlers
✓ Middleware, interceptors, or decorators that process the changed code
✓ Core utility functions with complex logic that changed code depends on
✓ Shared state, singletons, or global variables accessed by the changes

EXCLUDE files that are:
✗ Documentation files (README.md, CHANGELOG.md, *.txt, *.rst, docs/*)
✗ Configuration files (package.json, go.mod, .gitignore, docker-compose.yml, *.config.js)
✗ Standard library imports (e.g., "fmt", "os", "react", "lodash")
✗ External dependencies from node_modules, vendor, site-packages
✗ Test files UNLESS the diff modifies production code that those tests directly validate
✗ Build scripts, CI/CD configs (Makefile, .github/, .gitlab-ci.yml)
✗ Static assets (images, fonts, CSS-only files) unless they affect code logic
✗ Files not present in the "Repository Structure" above
✗ Changed files already listed in "ALREADY AVAILABLE" section

PRIORITIZATION (request max 12 files, highest priority first):
1. **Direct imports/dependencies** (15 files imported/called by changed code)
2. **Type definitions** (10 - interfaces, structs, classes used in changes)
3. **Parent/base classes** (8 - inheritance hierarchy for changed classes)
4. **Shared utilities** (6 - helper functions used by changes)
5. **API/Route definitions** (4 - if changes implement endpoints)
6. **Database models** (3 - if changes query/modify data)
7. **Middleware** (2 - if changes are processed by middleware)

SMART FILTERING:
- If a changed file is self-contained (no external calls), return empty array
- If imports are simple (just stdlib), no dependencies needed
- If file only has isolated logic, skip dependencies
- Prefer fewer, more relevant files over comprehensive coverage

═══════════════════════════════════════════════════════════════════════════════
OUTPUT REQUIREMENTS
═══════════════════════════════════════════════════════════════════════════════

Return ONLY valid JSON matching this schema:
{
  "files": ["path/to/file1.go", "path/to/file2.ts"]
}

Rules:
- Return empty array if no additional files are needed: {"files": []}
- Maximum 12 files (prioritize most critical for code validation)
- ONLY include actual code files (no docs, no configs)
- Paths must exactly match those in "Repository Structure"
- No markdown formatting, no explanations, only JSON

BEGIN ANALYSIS NOW.
`, diff, fileList, repoStructure)

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("🔭 [SCOUT PASS] - PROMPT CONTEXT")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("Diff Size: %d bytes\n", len(diff))
	fmt.Printf("Reviewable Code Files: %d\n", len(fileList))

	// Prepare Request
	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"responseMimeType": "application/json",
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s?key=%s", geminiURL, apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM scout failed status %d: %s", resp.StatusCode, string(body))
	}

	// Parse
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, err
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return []string{}, nil
	}

	responseText := geminiResp.Candidates[0].Content.Parts[0].Text

	fmt.Println("\n" + strings.Repeat("*", 80))
	fmt.Println("📥 [SCOUT PASS] - RAW LLM RESPONSE")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Println(responseText)
	fmt.Println(strings.Repeat("*", 80) + "\n")

	// Extraction logic to handle markdown backticks
	jsonStr := responseText
	if idx := strings.Index(jsonStr, "```json"); idx != -1 {
		jsonStr = jsonStr[idx+7:]
		if endIdx := strings.Index(jsonStr, "```"); endIdx != -1 {
			jsonStr = jsonStr[:endIdx]
		}
	} else if idx := strings.Index(jsonStr, "```"); idx != -1 {
		jsonStr = jsonStr[idx+3:]
		if endIdx := strings.Index(jsonStr, "```"); endIdx != -1 {
			jsonStr = jsonStr[:endIdx]
		}
	}
	jsonStr = strings.TrimSpace(jsonStr)

	var result struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		fmt.Printf("⚠️ Scout JSON Parse Failed: %v | Response: %s\n", err, responseText)
		return nil, fmt.Errorf("failed to parse scout JSON: %v", err)
	}

	// Filter out any documentation files that might have slipped through
	filteredFiles := []string{}
	for _, file := range result.Files {
		if !isDocumentationFile(file) {
			filteredFiles = append(filteredFiles, file)
		}
	}

	fmt.Println("✅ [SCOUT PASS] - IDENTIFIED DEPENDENCIES")
	fmt.Printf("Files: %v\n", filteredFiles)
	if len(filteredFiles) != len(result.Files) {
		fmt.Printf("Filtered out %d doc/config files\n", len(result.Files)-len(filteredFiles))
	}
	fmt.Println(strings.Repeat("=", 80) + "\n")

	return filteredFiles, nil
}