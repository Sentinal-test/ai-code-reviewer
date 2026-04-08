### GITLAB reviews

ai-code-reviewer @project_81112965_bot_121f85bcaa1418b758c7f907c3ac36fa started a thread on the diff 14 minutes ago
.gitlab-ci.yml  0 → 100644
    # Fetch target branch for diffing
    - git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" --depth=100 || true
    # Ensure merge base exists
    - |
      if ! git merge-base "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" "$CI_COMMIT_SHA" >/dev/null 2>&1; then
        echo "⚠️ No merge base found. Fetching more history..."
        git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" --depth=500 || true
        if ! git merge-base "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" "$CI_COMMIT_SHA" >/dev/null 2>&1; then
          echo "⚠️ Still no merge base. Fetching full history..."
          git fetch --prune --unshallow || true
        fi
      fi
  script:
    - echo "🏗️ Building AI Reviewer..."
    - cd backend
    - go build -o ../ai-reviewer cmd/cli/main.go
ai-code-reviewer
ai-code-reviewer
@project_81112965_bot_121f85bcaa1418b758c7f907c3ac36fa
14 minutes ago
Owner
[CRITICAL] security

Building and executing the reviewer binary directly from the merge request branch allows an attacker to execute arbitrary code and exfiltrate secrets (like GITLAB_BOT_TOKEN). Build the binary from a trusted base branch before running it against the PR diff.

Reply…
ai-code-reviewer
ai-code-reviewer @project_81112965_bot_121f85bcaa1418b758c7f907c3ac36fa started a thread on the diff 14 minutes ago
.gitlab-ci-template.yml  0 → 100644
    # Ensure target branch is available for diffing
    - git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" --depth=100 || true
    # Ensure merge base exists (handles shallow clones)
    - |
      if ! git merge-base "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" "$CI_COMMIT_SHA" >/dev/null 2>&1; then
        echo "⚠️ No merge base found. Fetching more history..."
        git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" --depth=500 || true
        if ! git merge-base "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" "$CI_COMMIT_SHA" >/dev/null 2>&1; then
          echo "⚠️ Still no merge base. Fetching full history..."
          git fetch --prune --unshallow || true
        fi
      fi
  script:
    - echo "🏗️ Building AI Reviewer..."
    - cd backend
    - go build -o ../ai-reviewer cmd/cli/main.go
ai-code-reviewer
ai-code-reviewer
@project_81112965_bot_121f85bcaa1418b758c7f907c3ac36fa
14 minutes ago
Owner
[CRITICAL] security

Option 1 builds and executes the reviewer binary directly from the current branch. If this template is used in the reviewer's own repository, it allows attackers to exfiltrate secrets by modifying main.go in a PR. Update Option 1 to clone the reviewer from a trusted repository or build from a trusted branch.

Reply…
ai-code-reviewer
ai-code-reviewer @project_81112965_bot_121f85bcaa1418b758c7f907c3ac36fa started a thread on the diff 14 minutes ago
backend/internal/platform/gitlab/gitlab.go  0 → 100644
	if err := g.doJSON(ctx, http.MethodGet, fmt.Sprintf("/projects/%s/merge_requests/%d", project, mrIID), nil, nil, &mr); err != nil {
		return nil, err
	}

	var commits []gitLabCommit
	if err := g.doJSON(ctx, http.MethodGet, fmt.Sprintf("/projects/%s/merge_requests/%d/commits", project, mrIID), nil, nil, &commits); err != nil {
		fmt.Printf("⚠️ Failed to fetch GitLab MR commits for !%d: %v\n", mrIID, err)
	}

	var messages []string
	if len(commits) > 0 {
		start := 0
		if len(commits) > 5 {
			start = len(commits) - 5
		}
		for _, commit := range commits[start:] {
ai-code-reviewer
ai-code-reviewer
@project_81112965_bot_121f85bcaa1418b758c7f907c3ac36fa
14 minutes ago
Owner
[WARNING] logic

GitLab's merge request commits API returns commits in reverse chronological order (newest first) by default. By taking commits[start:] where start = len(commits) - 5, this logic selects the 5 oldest commits instead of the most recent ones. Use commits[:min(5, len(commits))] instead.

Reply…
ai-code-reviewer
ai-code-reviewer @project_81112965_bot_121f85bcaa1418b758c7f907c3ac36fa started a thread on the diff 14 minutes ago
backend/internal/platform/gitlab/gitlab.go  0 → 100644
					file = note.Position.OldPath
				}
				if note.Position.NewLine != nil {
					line = *note.Position.NewLine
				} else if note.Position.OldLine != nil {
					line = *note.Position.OldLine
				}
			}
			if pf := memory.ParseMarker(note.Body, file, line, note.ID); pf != nil {
				pf.ThreadID = discussion.ID
				findings = append(findings, *pf)
			}
		}
	}

	notes, err := fetchPaginated[gitLabMRNote](ctx, g, fmt.Sprintf("/projects/%s/merge_requests/%d/notes", project, mrIID))
ai-code-reviewer
ai-code-reviewer
@project_81112965_bot_121f85bcaa1418b758c7f907c3ac36fa
14 minutes ago
Owner
[WARNING] architecture

Fetching /notes is redundant because the /discussions endpoint already returns all notes for the merge request (including individual notes). This causes duplicate API calls and duplicate findings in the previousFindings array.

Reply…
ai-code-reviewer
ai-code-reviewer
@project_81112965_bot_121f85bcaa1418b758c7f907c3ac36fa
14 minutes ago
Owner
AI Code Review Summary
Found critical security vulnerabilities in the CI/CD pipeline configurations allowing secret exfiltration, along with logic and architectural issues in the GitLab API integration.

📊 Coverage: 16/16 files reviewed

✅ Resolved Issues
The switch statement for SCM providers was refactored into a cleaner interface-based approach using platform.ReviewPlatform.
The extractValidDiffLines function was moved to a shared utils package and is now used correctly by both GitHub and GitLab clients.
The logic to build the marker comment body was consolidated into memory.BuildMarkerCommentBody.
The type switching logic was removed in favor of the platform.ReviewPlatform interface.

💰 [LLM Cost] Review cost summary
═══════════════════════════════════════════════════════════════════════════════
Provider/Model: gemini / gemini-3.1-pro-preview-customtools
Generate calls: 18
Tokens: input=2120722 (cached=1480959), output=2254, tool=0, thoughts=129934
Cost (USD): $1.602766 total
  - Input (non-cached): $1.279526
  - Cached input reads: $0.296192
  - Output:             $0.027048
  - Cache storage:      $0.000000 (creates=0, storedTokens=0, ttlHoursSum=0.000)


### GITHUB reviews 


AI Code Review Summary
Found several security issues including token exposure via CLI flags, unbounded memory allocations, and HTML injection, along with a logic bug in commit fetching and a structural issue with incomplete provider abstraction.

📊 Coverage: 18/18 files reviewed

backend/internal/platform/gitlab/gitlab.go
		if len(commits) > 5 {
			start = len(commits) - 5
		}
		for _, commit := range commits[start:] {
@github-actions
github-actions bot
15 minutes ago
[WARNING] bug

GitLab API returns commits in reverse chronological order (newest first). Taking commits[start:] extracts the oldest commits instead of the newest. Use commits[:5] to get the most recent commits.

@tegveer-work	Reply...
backend/cmd/cli/main.go
	apiKeyFlag := flag.String("api-key", "", "API Key (default Gemini, fallback for others)")
	llmProviderFlag := flag.String("llm-provider", "gemini", "LLM Provider (gemini, openai, claude)")
	scmProviderFlag := flag.String("scm-provider", "", "SCM provider (github, gitlab)")
	tokenFlag := flag.String("token", "", "SCM API token")
@github-actions
github-actions bot
15 minutes ago
[WARNING] security

Lines 59, .gitlab-ci-template.yml:56, .gitlab-ci.yml:42: Passing sensitive secrets (--token) via command-line flags exposes them to process lists (ps aux) and potentially CI logs. Rely exclusively on environment variables for secrets.

@tegveer-work	Reply...
backend/internal/memory/memory.go
// BuildMarkerCommentBody formats the review comment body with a hidden deduplication marker.
func BuildMarkerCommentBody(c models.ReviewComment) string {
	encodedFile := base64.RawURLEncoding.EncodeToString([]byte(c.File))
	marker := fmt.Sprintf("<!-- ai-reviewer:v1 file=%s line=%d severity=%s layer=%s hash=%s -->",
@github-actions
github-actions bot
15 minutes ago
[WARNING] security

The LLM-generated Severity and Layer fields are embedded directly into an HTML comment. If the LLM outputs -->, it will prematurely close the comment and could allow Markdown/HTML injection. Sanitize these fields by removing or encoding -->.

@tegveer-work	Reply...
backend/internal/platform/gitlab/gitlab.go
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
@github-actions
github-actions bot
15 minutes ago
[WARNING] security

Using io.ReadAll on an HTTP response body without a limit can lead to memory exhaustion (OOM) if the server returns a massive response. Use io.LimitReader to bound the maximum bytes read.

@tegveer-work	Reply...
backend/internal/platform/gitlab/gitlab.go
	}, nil
}

func fetchPaginated[T any](ctx context.Context, g *GitLabClient, path string) ([]T, error) {
@github-actions
github-actions bot
15 minutes ago
[WARNING] performance

Unbounded pagination can lead to memory exhaustion (OOM) if the API returns an excessively large number of pages. Implement a maximum page limit or item count to bound memory usage.

@tegveer-work	Reply...
backend/internal/platform/github/github.go
}

func (g *GitHubClient) FetchPreviousFindings(ctx context.Context, id int) ([]models.PreviousFinding, error) {
	return memory.FetchPreviousFindings(ctx, g.client, g.owner, g.repo, id)
@github-actions
github-actions bot
15 minutes ago
[INFO] architecture

The memory package is intended to be provider-neutral, but memory.FetchPreviousFindings still contains GitHub-specific API calls. Move the GitHub-specific retrieval logic into platform/github/github.go to complete the architectural separation.

💰 [LLM Cost] Review cost summary
═══════════════════════════════════════════════════════════════════════════════
Provider/Model: gemini / gemini-3.1-pro-preview-customtools
Generate calls: 15
Tokens: input=1694498 (cached=951790), output=108718, tool=0, thoughts=53151
Cost (USD): $2.980390 total
  - Input (non-cached): $1.485416
  - Cached input reads: $0.190358
  - Output:             $1.304616
  - Cache storage:      $0.000000 (creates=0, storedTokens=0, ttlHoursSum=0.000)

