# AI Code Reviewer — GitLab Usage Guide

This guide explains how to integrate the AI Code Reviewer into your GitLab CI/CD pipelines. The reviewer runs **entirely within your GitLab Runner** — your code never leaves your infrastructure.

> **Already using this on GitHub?** The same review engine, prompts, and accuracy apply. Only the delivery layer differs.

---

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Quick Start (Single Project)](#quick-start-single-project)
3. [Organization / Group Rollout](#organization--group-rollout)
4. [Self-Hosted GitLab](#self-hosted-gitlab)
5. [Configuration Reference](#configuration-reference)
6. [Custom Review Rules](#custom-review-rules)
7. [How It Works](#how-it-works)
8. [Security Assurance](#security-assurance)
9. [Troubleshooting](#troubleshooting)

---

## Prerequisites

### 1. LLM API Key

You need an API key from at least one provider:

| Provider  | Get Key | Variable Name |
|-----------|---------|---------------|
| **Gemini** (default, recommended) | [Google AI Studio](https://aistudio.google.com/app/apikey) | `GEMINI_API_KEY` |
| OpenAI | [OpenAI Platform](https://platform.openai.com/api-keys) | `OPENAI_API_KEY` |
| Anthropic | [Anthropic Console](https://console.anthropic.com/) | `ANTHROPIC_API_KEY` |

### 2. GitLab Access Token

The reviewer needs a token with **`api`** scope to post comments on merge requests.

**Option A: Project Access Token** (single project)
1. Go to your project → **Settings → Access Tokens**
2. Click **Add new token**
3. Name: `ai-code-reviewer`
4. Role: **Developer** (minimum) or **Maintainer**
5. Scopes: ✅ **api**
6. Click **Create project access token**
7. **Copy the token immediately** (you won't see it again)

**Option B: Group Access Token** (organization-wide — recommended)
1. Go to your group → **Settings → Access Tokens**
2. Click **Add new token**
3. Name: `ai-code-reviewer-bot`
4. Role: **Developer** or **Maintainer**
5. Scopes: ✅ **api**
6. Click **Create group access token**
7. **Copy the token immediately**

> **Note:** Group tokens grant access to all projects in the group. This is the GitLab equivalent of a GitHub App installation.

### 3. Add CI/CD Variables

Add your secrets as CI/CD variables:

**For a single project:**
- Go to **Settings → CI/CD → Variables → Expand → Add variable**

**For your entire group (recommended):**
- Go to **Group → Settings → CI/CD → Variables → Expand → Add variable**

Add these variables:

| Variable | Value | Settings |
|----------|-------|----------|
| `GITLAB_BOT_TOKEN` | Your access token | ✅ Mask variable, ✅ Protected (optional) |
| `GEMINI_API_KEY` | Your Gemini API key | ✅ Mask variable, ✅ Protected (optional) |

> **Tip:** Setting "Protected" restricts the variable to protected branches only. Uncheck it if you want the reviewer to run on all MR branches.

---

## Quick Start (Single Project)

### Step 1: Get the Reviewer Source

Clone or fork the AI Code Reviewer repository into your GitLab group:

```bash
git clone https://github.com/your-org/ai-code-reviewer.git
cd ai-code-reviewer
git remote set-url origin https://gitlab.com/your-group/ai-code-reviewer.git
git push -u origin main
```

Or if it's already on GitHub and you just want to use it:

```bash
# You'll reference the backend code in your CI job
```

### Step 2: Add `.gitlab-ci.yml` to Your Project

Create (or extend) your `.gitlab-ci.yml`:

```yaml
stages:
  - review

variables:
  GIT_DEPTH: "0"   # Full clone for accurate diffs

ai_code_review:
  stage: review
  image: golang:1.24
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
  cache:
    key: "ai-reviewer-${CI_PROJECT_ID}"
    paths:
      - .ai-reviewer/
  before_script:
    - git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" --depth=100 || true
    - |
      if ! git merge-base "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" "$CI_COMMIT_SHA" >/dev/null 2>&1; then
        git fetch --prune --unshallow || true
      fi
    # Clone the reviewer and build
    - git clone --depth=1
        "https://gitlab-ci-token:${GITLAB_BOT_TOKEN}@gitlab.com/your-group/ai-code-reviewer.git"
        /tmp/ai-reviewer-src
    - cd /tmp/ai-reviewer-src/backend
    - go build -o /usr/local/bin/ai-reviewer cmd/cli/main.go
    - cd "$CI_PROJECT_DIR"
  script:
    - ai-reviewer
        --scm-provider gitlab
        --token "$GITLAB_BOT_TOKEN"
        --api-url "$CI_API_V4_URL"
        --project-id "$CI_PROJECT_ID"
        --repo "$CI_PROJECT_PATH"
        --change-request "$CI_MERGE_REQUEST_IID"
        --sha "$CI_COMMIT_SHA"
        --base "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME"
        --head "$CI_COMMIT_SHA"
```

### Step 3: Create a Merge Request

Push a branch, open a merge request, and the review will run automatically!

---

## Organization / Group Rollout

This is the GitLab equivalent of your GitHub org setup where you have a private repo with the action, added API keys to org secrets, and allow the action to be reused.

### Architecture

```
your-gitlab-group/
├── ci-templates/          ← Shared repo with the reusable CI job
│   └── templates/
│       └── ai-review.yml  ← The reusable job definition
├── ai-code-reviewer/      ← The reviewer source code (private)
├── project-a/             ← Any project: 3 lines to enable
├── project-b/             ← Any project: 3 lines to enable
└── project-c/             ← Any project: 3 lines to enable
```

### Step 1: Push the Reviewer Source to Your Group

```bash
# Mirror the reviewer into your GitLab group
git clone https://github.com/your-org/ai-code-reviewer.git
cd ai-code-reviewer
git remote add gitlab https://gitlab.com/your-group/ai-code-reviewer.git
git push gitlab main
```

### Step 2: Create the CI Templates Project

Create a new project: `your-group/ci-templates`

Add the file `templates/ai-review.yml`:

```yaml
# templates/ai-review.yml
# Reusable AI Code Review job template

.ai_review_template:
  stage: review
  image: golang:1.24
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
  variables:
    LLM_PROVIDER: "gemini"
    AI_REVIEWER_REPO: "your-group/ai-code-reviewer"
    AI_REVIEWER_REF: "main"
  cache:
    key: "ai-reviewer-${CI_PROJECT_ID}"
    paths:
      - .ai-reviewer/
  before_script:
    - git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" --depth=100 || true
    - |
      if ! git merge-base "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" "$CI_COMMIT_SHA" >/dev/null 2>&1; then
        git fetch --prune --unshallow || true
      fi
    - git clone --depth=1 --branch "$AI_REVIEWER_REF"
        "https://gitlab-ci-token:${GITLAB_BOT_TOKEN}@${CI_SERVER_HOST}/${AI_REVIEWER_REPO}.git"
        /tmp/ai-reviewer-src
    - cd /tmp/ai-reviewer-src/backend
    - go build -o /usr/local/bin/ai-reviewer cmd/cli/main.go
    - cd "$CI_PROJECT_DIR"
  script:
    - ai-reviewer
        --scm-provider gitlab
        --token "$GITLAB_BOT_TOKEN"
        --api-url "$CI_API_V4_URL"
        --project-id "$CI_PROJECT_ID"
        --repo "$CI_PROJECT_PATH"
        --change-request "$CI_MERGE_REQUEST_IID"
        --sha "$CI_COMMIT_SHA"
        --base "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME"
        --head "$CI_COMMIT_SHA"
        --llm-provider "$LLM_PROVIDER"
```

### Step 3: Add Group-Level CI/CD Variables

Go to **your-group → Settings → CI/CD → Variables** and add:

| Variable | Value | Masked | Protected |
|----------|-------|--------|-----------|
| `GITLAB_BOT_TOKEN` | Group access token | ✅ | ❌ (uncheck to allow all branches) |
| `GEMINI_API_KEY` | Your Gemini API key | ✅ | ❌ |

### Step 4: Enable in Any Project (3 Lines!)

Any project in the group just needs to add this to their `.gitlab-ci.yml`:

```yaml
include:
  - project: 'your-group/ci-templates'
    file: '/templates/ai-review.yml'
    ref: 'main'

stages:
  - review

ai_code_review:
  extends: .ai_review_template
```

That's it! Anyone in the group can enable AI reviews by adding these 8 lines. The secrets are inherited from the group level.

### Step 5: Customize Per-Project (Optional)

Projects can customize behavior by overriding variables or adding review rules:

```yaml
include:
  - project: 'your-group/ci-templates'
    file: '/templates/ai-review.yml'
    ref: 'main'

stages:
  - review

ai_code_review:
  extends: .ai_review_template
  variables:
    LLM_PROVIDER: "openai"        # Use a different LLM
  script:
    - ai-reviewer
        --scm-provider gitlab
        --token "$GITLAB_BOT_TOKEN"
        --api-url "$CI_API_V4_URL"
        --project-id "$CI_PROJECT_ID"
        --repo "$CI_PROJECT_PATH"
        --change-request "$CI_MERGE_REQUEST_IID"
        --sha "$CI_COMMIT_SHA"
        --base "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME"
        --head "$CI_COMMIT_SHA"
        --llm-provider "$LLM_PROVIDER"
        --review-rules |
          instructions:
            - "Focus on Go-specific concurrency issues"
          ignore:
            - "vendor/"
            - "*_test.go"
          focus:
            - "internal/api"
```

---

## Self-Hosted GitLab

For self-hosted GitLab instances, the reviewer automatically uses the correct API URL via the `CI_API_V4_URL` predefined variable.

No additional configuration is needed — the `--api-url "$CI_API_V4_URL"` flag handles this automatically.

If your instance uses a custom CA certificate, add it to the runner's trust store or set:
```yaml
variables:
  GIT_SSL_NO_VERIFY: "true"   # Only for testing — not recommended for production
```

---

## Configuration Reference

### CLI Flags (GitLab-specific)

| Flag | CI Variable | Description |
|------|-------------|-------------|
| `--scm-provider` | `SCM_PROVIDER` | Must be `gitlab` (auto-detected in GitLab CI) |
| `--token` | `GITLAB_BOT_TOKEN` | GitLab access token with `api` scope |
| `--api-url` | `CI_API_V4_URL` | GitLab API base URL (automatic) |
| `--project-id` | `CI_PROJECT_ID` | GitLab project numeric ID (automatic) |
| `--repo` | `CI_PROJECT_PATH` | Project path e.g. `group/project` (automatic) |
| `--change-request` | `CI_MERGE_REQUEST_IID` | Merge request IID (automatic) |
| `--sha` | `CI_COMMIT_SHA` | Current commit SHA (automatic) |
| `--base` | — | Base ref for diff (default: `origin/main`) |
| `--head` | — | Head ref for diff (default: `HEAD`) |

### Common Flags (Shared with GitHub)

| Flag | Description |
|------|-------------|
| `--llm-provider` | `gemini` (default), `openai`, or `claude` |
| `--dry-run` | Print results to stdout without posting |
| `--no-cache` | Skip code graph cache |
| `--review-rules` | YAML string with custom review rules |

### Auto-Detection

The reviewer **automatically detects GitLab** when running in GitLab CI. You don't need to pass `--scm-provider gitlab` explicitly if these environment variables are present:

- `CI_MERGE_REQUEST_IID` — set by GitLab for merge request pipelines
- `GITLAB_CI=true` — always set in GitLab CI

---

## Custom Review Rules

You can pass custom review rules to fine-tune what the reviewer focuses on:

```yaml
ai_code_review:
  extends: .ai_review_template
  script:
    - ai-reviewer
        --scm-provider gitlab
        --token "$GITLAB_BOT_TOKEN"
        --api-url "$CI_API_V4_URL"
        --project-id "$CI_PROJECT_ID"
        --repo "$CI_PROJECT_PATH"
        --change-request "$CI_MERGE_REQUEST_IID"
        --sha "$CI_COMMIT_SHA"
        --base "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME"
        --head "$CI_COMMIT_SHA"
        --review-rules "instructions:
          - Focus on security vulnerabilities
          - Check for SQL injection and XSS
        ignore:
          - 'vendor/'
          - '*.generated.go'
          - 'docs/'
        focus:
          - 'internal/api/'
          - 'cmd/server/'"
```

### Rules Fields

| Field | Description |
|-------|-------------|
| `instructions` | Custom instructions for the LLM reviewers |
| `ignore` | File patterns to exclude from review (glob syntax) |
| `focus` | File patterns to prioritize in review |

---

## How It Works

```
┌─────────────────────────────────────────────────────────┐
│                   GitLab Runner                          │
│                                                         │
│  1. git diff → Local diff extraction                    │
│  2. Code Graph → Dependency analysis                    │
│  3. Chunker → Smart file grouping                       │
│  4. Orchestrator → 3 specialist agents in parallel      │
│     ├── Correctness Agent (bugs, logic errors)          │
│     ├── Security Agent (vulnerabilities, trust)         │
│     └── Structure Agent (architecture, APIs)            │
│  5. Consolidator → Dedup + rank findings                │
│  6. GitLab API → Post inline discussions + summary      │
│                                                         │
│  External call: Only to LLM API (Gemini/OpenAI/Claude)  │
└─────────────────────────────────────────────────────────┘
```

### Feature Parity with GitHub

| Feature | GitHub | GitLab |
|---------|--------|--------|
| Inline comments on changed lines | ✅ PR review comments | ✅ MR diff discussions |
| Summary comment | ✅ PR review body | ✅ MR general note |
| Finding deduplication (memory) | ✅ Comment markers | ✅ Same markers |
| Auto-resolve fixed issues | ✅ GraphQL thread resolution | ✅ Discussion resolution |
| Line snapping (nearest valid line) | ✅ | ✅ |
| Fallback to general comment | ✅ | ✅ |
| Code Graph caching | ✅ GitHub Actions cache | ✅ GitLab CI cache |
| Multi-LLM support | ✅ | ✅ |
| Custom review rules | ✅ | ✅ |
| Rate-limit protection | ✅ | ✅ |

---

## Security Assurance

When running in GitLab CI:

- ✅ **No source code leaves your runner** — all git operations are local
- ✅ **No webhooks** — triggered by GitLab CI merge request pipelines
- ✅ **Only external call is to the LLM provider** (Google/OpenAI/Anthropic API)
- ✅ **Token scope is minimal** — only `api` for posting comments
- ✅ **Secrets are masked** — GitLab masks CI/CD variables in logs
- ✅ **Same review engine** — identical accuracy to the GitHub version

---

## Troubleshooting

### "Error getting diff"

Ensure `GIT_DEPTH: "0"` is set in your pipeline variables and the `before_script` fetches the target branch:

```yaml
variables:
  GIT_DEPTH: "0"

before_script:
  - git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" --depth=100 || true
```

### "gitlab api POST .../discussions failed (HTTP 400)"

The inline comment position doesn't match the MR diff. This can happen when:
- The diff has been updated since the pipeline started
- The line number is in a context (unchanged) section

The reviewer automatically falls back to a general MR note in this case.

### "gitlab api ... failed (HTTP 401)"

Your `GITLAB_BOT_TOKEN` is missing, expired, or doesn't have the `api` scope.

### "gitlab api ... failed (HTTP 403)"

The token's role doesn't have permission to post to this project. Ensure:
- For project tokens: role is **Developer** or higher
- For group tokens: the project is within the group

### Pipeline doesn't trigger on MRs

Ensure your job has the correct rule:
```yaml
rules:
  - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
```

And that your project has **Merge Request Pipelines** enabled (Settings → CI/CD → General pipelines).

### Variables are empty in logs

If `CI_MERGE_REQUEST_IID` is empty, the pipeline wasn't triggered by a merge request event. Check that:
1. The job uses `rules: - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'`
2. You're not using `only: [branches]` which runs branch pipelines, not MR pipelines

### Review takes too long

For large MRs with many changed files:
- The reviewer chunks files intelligently, but very large PRs (50+ files) take proportionally longer
- Consider adding `--review-rules` with an `ignore` list for generated files, vendor directories, etc.

---

## Comparison: GitHub vs GitLab Setup

| Aspect | GitHub | GitLab |
|--------|--------|--------|
| Auth mechanism | GitHub App or PAT | Project/Group Access Token |
| Secret storage | Org/Repo Secrets | Group/Project CI/CD Variables |
| Trigger | `on: pull_request` | `rules: - if: merge_request_event` |
| Reusable config | `uses: owner/repo@tag` | `include: project: ...` |
| CI runner | GitHub-hosted runners | GitLab shared/self-hosted runners |
| Inline comments | PR review comments | MR diff discussions |
| Thread resolution | GraphQL `resolveReviewThread` | REST discussion resolve |

---

## Next Steps

- ⭐ **Star the project** if you find it useful
- 🐛 **Report issues** on the project's issue tracker
- 📖 Read [GITHUB_ACTION_USAGE.md](./GITHUB_ACTION_USAGE.md) for the GitHub version
- 🏗️ Read [ARCHITECTURE.md](./ARCHITECTURE.md) for technical details
