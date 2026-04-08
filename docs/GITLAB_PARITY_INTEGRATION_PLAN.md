# GitLab Integration Plan With GitHub Parity

## Goal

Run the same reviewer on both GitHub and GitLab with the same review engine, same prompts, same chunking/orchestration, same developer rules, and the same previous-finding memory behavior.

The review logic should stay single-source. Only the forge adapter should differ.

## Current State

The core review engine is already portable:

- Diff collection from local git: `backend/internal/action/git.go`
- Chunking: `backend/internal/chunker/chunker.go`
- Code graph/context: `backend/internal/codegraph/*`
- Specialist agents and consolidator: `backend/internal/agents/*`, `backend/internal/orchestrator/*`, `backend/internal/llm/*`

The delivery layer is still GitHub-specific:

- GitHub composite action entrypoint: `action.yml`
- GitHub-only CLI args/env resolution: `backend/cmd/cli/main.go`
- GitHub comment posting and review batching: `backend/internal/action/github.go`
- GitHub thread resolution via GraphQL: `backend/internal/action/resolve.go`
- GitHub previous-finding fetch: `backend/internal/memory/memory.go`
- GitHub API wrapper: `backend/internal/github/client.go`
- GitHub App based multi-repo discovery: `backend/internal/multirepo/discovery.go`
- GitHub clone auth format for peer repos: `backend/internal/multirepo/checkout.go`

## What "Same Functionality And Accuracy" Actually Means

To keep accuracy the same, these parts must remain identical across GitHub and GitLab:

- Same diff content fed to the LLM
- Same chunking boundaries
- Same orchestrator flow: Orchestrator -> Correctness/Security/Structure -> Consolidator
- Same prompts and tool loop behavior
- Same dependency/context gathering
- Same deduplication and previous-finding memory logic
- Same developer rules input

To keep functionality the same, GitLab must also support equivalents for:

- Inline review comments on changed lines
- Summary comment on the merge request
- Previous-finding lookup
- Auto-resolution of earlier findings when the consolidator marks them resolved
- Multi-repo enrichment, if you want full parity with current GitHub App mode

One caveat: GitHub "check runs" do not exist in exactly the same form in GitLab. The equivalent should be:

- GitHub: PR review comments + check/pipeline result
- GitLab: MR discussions + pipeline job result

That is platform-equivalent behavior, even if the UI object names differ.

## Recommended Architecture

Create a forge adapter layer and keep the review engine unchanged.

Suggested interface:

```go
type ReviewPlatform interface {
    Provider() string
    LoadRequestContext(ctx context.Context, id string) (*models.PRContext, error)
    LoadPreviousFindings(ctx context.Context, id string) ([]models.PreviousFinding, error)
    PostReview(ctx context.Context, id string, result *models.ReviewResult, diff string, commitSHA string) error
    ResolveFindings(ctx context.Context, id string, resolutions []models.Resolution, previous []models.PreviousFinding) error
    ListAccessibleRepos(ctx context.Context) ([]multirepo.DiscoveredRepo, error)
}
```

Recommended package split:

- `backend/internal/platform/platform.go`
- `backend/internal/platform/github/*`
- `backend/internal/platform/gitlab/*`

Do not fork the orchestrator, chunker, or prompts for GitLab.

## Code Changes Required

### 1. Make the CLI forge-aware

Refactor `backend/cmd/cli/main.go`.

Add generic flags/env support:

- `--scm-provider=github|gitlab`
- `--repo`
- `--change-request`
- `--sha`
- `--base`
- `--head`
- `--token`
- `--api-url`
- `--project-id`

Then map provider-specific CI vars into those generic inputs:

- GitHub:
  - `GITHUB_REPOSITORY`
  - `PR_NUMBER`
  - `GITHUB_SHA`
- GitLab:
  - `CI_PROJECT_PATH`
  - `CI_PROJECT_ID`
  - `CI_MERGE_REQUEST_IID`
  - `CI_COMMIT_SHA`
  - `CI_API_V4_URL`

The rest of the CLI should call a `ReviewPlatform`, not `GitHubClient` directly.

### 2. Move GitHub logic behind the interface

Refactor these files into a GitHub adapter implementation:

- `backend/internal/action/github.go`
- `backend/internal/action/resolve.go`
- `backend/internal/github/client.go`
- GitHub branch of previous-finding retrieval from `backend/internal/memory/memory.go`

Result:

- GitHub behavior stays exactly as-is
- GitLab can implement the same contract

### 3. Add a GitLab adapter

Create GitLab-specific support for:

- Fetch MR metadata
- Fetch previous MR discussions/notes created by the bot
- Post inline diff discussions
- Post summary MR note
- Resolve previously created discussions when a finding is marked resolved

Recommended files:

- `backend/internal/platform/gitlab/client.go`
- `backend/internal/platform/gitlab/review.go`
- `backend/internal/platform/gitlab/resolve.go`
- `backend/internal/platform/gitlab/memory.go`

### 4. Keep previous-finding memory format provider-neutral

Keep the hidden marker strategy from `backend/internal/memory/memory.go`.

Continue embedding a stable marker in every comment, for example:

```html
<!-- ai-reviewer:v1 file=... line=... severity=... layer=... hash=... -->
```

That gives identical deduplication behavior on both platforms.

Only the transport changes:

- GitHub: PR review comment / issue comment
- GitLab: MR discussion note / MR note

### 5. Implement GitLab line-position mapping

GitHub currently uses local diff parsing and line snapping in `backend/internal/action/github.go`.

GitLab needs the same validation idea, plus GitLab discussion positions built from the latest MR diff version. The GitLab adapter should:

1. Build the diff locally exactly as today.
2. Reuse the same line-snapping logic.
3. Translate the final file/line into GitLab discussion `position` fields using the latest MR diff refs.
4. Fall back to a general MR note when inline placement is impossible.

This is critical for parity. If you skip this, GitLab will have worse placement than GitHub.

### 6. Add GitLab previous-comment resolution

GitHub resolution currently depends on comment node IDs and GraphQL thread resolution in `backend/internal/action/resolve.go`.

GitLab should instead:

- load prior discussions for the MR,
- map bot markers back to discussion IDs,
- resolve discussions whose findings are now marked resolved,
- skip discussions already resolved.

This gives the same "fixed issue gets closed automatically" behavior.

### 7. Make multi-repo enrichment provider-neutral

Current multi-repo mode is GitHub App dependent:

- repo enumeration in `backend/internal/multirepo/discovery.go`
- tokenized clone URL in `backend/internal/multirepo/checkout.go`

To keep parity on GitLab:

- change discovery input to a provider-neutral repo struct
- let each provider enumerate accessible peer repos
- make clone auth strategy provider-specific

For GitLab, use a bot token or group/project access token with repository read access across the relevant group.

### 8. Split CI wrappers from the binary

Keep two thin CI entrypoints:

- GitHub: current `action.yml`
- GitLab: `.gitlab-ci.yml` template and possibly `scripts/run-gitlab-review.sh`

Both should only do:

- checkout/fetch enough history
- resolve secrets
- build the same Go binary
- call the same CLI with provider-specific env mapped into generic flags

## What Must Be Done On GitLab

### GitLab project/group setup

1. Create a bot identity or use a project/group access token.
2. Give it API access and repository read access to the target project.
3. If you need multi-repo enrichment, grant it access to sibling projects too.
4. Add CI/CD variables:
   - `GITLAB_TOKEN`
   - `GEMINI_API_KEY` or `OPENAI_API_KEY` or `ANTHROPIC_API_KEY`
5. Mark secrets protected/masked as appropriate for your branch policy.

### GitLab pipeline setup

Use merge request pipelines, not plain branch pipelines.

Recommended job rules:

- run only when `CI_PIPELINE_SOURCE == "merge_request_event"`
- skip regular branch pushes unless you explicitly want dry-run reviews

Also ensure full git history is available:

- `GIT_DEPTH: "0"`

Without enough history, diff parity will drift.

### GitLab API behavior you need

The GitLab adapter should use:

- Merge request metadata endpoints
- Merge request discussions endpoints
- Merge request versions/diff refs for inline comment positions

The adapter should not rely only on CI env vars for diff refs, because inline discussion placement is tied to MR diff versions.

### Example `.gitlab-ci.yml`

This is the target shape once the GitLab adapter exists:

```yaml
stages:
  - review

variables:
  GIT_DEPTH: "0"

ai_code_review:
  stage: review
  image: golang:1.24
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
  before_script:
    - git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" --depth=100 || true
  script:
    - cd backend
    - go build -o ../ai-reviewer cmd/cli/main.go
    - cd ..
    - ./ai-reviewer \
        --scm-provider gitlab \
        --token "$GITLAB_TOKEN" \
        --api-url "$CI_API_V4_URL" \
        --project-id "$CI_PROJECT_ID" \
        --repo "$CI_PROJECT_PATH" \
        --change-request "$CI_MERGE_REQUEST_IID" \
        --sha "$CI_COMMIT_SHA" \
        --base "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" \
        --head "$CI_COMMIT_SHA"
```

## Exact Parity Plan

### Phase 1: Refactor for a single review core

Goal:

- no GitHub-specific logic in `backend/cmd/cli/main.go`
- provider chosen at runtime
- all review logic stays shared

Deliverables:

- provider-neutral CLI inputs
- `ReviewPlatform` interface
- GitHub implementation migrated behind the interface

### Phase 2: GitLab review transport parity

Goal:

- same MR/PR metadata loading
- same deduplication markers
- same inline comment behavior
- same fallback summary behavior
- same resolved-finding behavior

Deliverables:

- GitLab adapter
- inline discussion posting
- MR summary note
- discussion resolution
- tests for marker parsing and line-position translation

### Phase 3: Multi-repo parity

Goal:

- keep current cross-repo enrichment available on both providers

Deliverables:

- provider-neutral repository discovery
- GitLab peer-project enumeration
- provider-specific authenticated clone handling

### Phase 4: Documentation and rollout

Deliverables:

- update `README.md`
- update `docs/GITHUB_ACTION_USAGE.md`
- add a dedicated `docs/GITLAB_USAGE.md`
- document supported scopes/permissions for both platforms

## Testing Checklist

You should not call GitLab support done until these pass:

- Same diff on GitHub and GitLab produces the same LLM input payload
- Same PR/MR produces the same chunk count and chunk boundaries
- Same prompts are used for all 4 agents
- Same finding marker format is emitted
- Duplicate findings are skipped on rerun
- Fixed findings are auto-resolved on rerun
- Invalid inline locations fall back to a general summary note
- Multi-repo enrichment works or is explicitly disabled on both platforms

## Risks

### 1. Inline diff positioning is the hardest part

GitHub and GitLab model review locations differently. This is the main implementation risk.

### 2. Multi-repo parity is not free

Current implementation assumes GitHub App style repo discovery and GitHub clone URLs. GitLab needs its own discovery/auth model.

### 3. UI parity is not object parity

You can keep behavior the same, but not every GitHub UI concept has a GitLab object with the same name.

## My Recommendation

Do not build a separate GitLab review path.

Build this instead:

- one review engine
- one provider-neutral CLI contract
- one GitHub adapter
- one GitLab adapter

That is the only clean way to keep functionality and accuracy aligned long-term.

## Minimal File Change List

These are the files I would treat as the required refactor set:

- `backend/cmd/cli/main.go`
- `backend/internal/action/github.go`
- `backend/internal/action/resolve.go`
- `backend/internal/github/client.go`
- `backend/internal/memory/memory.go`
- `backend/internal/multirepo/discovery.go`
- `backend/internal/multirepo/checkout.go`
- `action.yml`
- new GitLab adapter files under `backend/internal/platform/gitlab/`
- new GitLab usage doc

## Official GitLab References

- Merge request pipelines: https://docs.gitlab.com/ci/pipelines/merge_request_pipelines/
- Merge requests API: https://docs.gitlab.com/api/merge_requests/
- Discussions API: https://docs.gitlab.com/api/discussions/
