# AI Code Reviewer — Complete System Workflow

> A detailed, end-to-end reference document describing every stage of the review pipeline, from PR trigger to GitHub comment, including all internal limits, token budgets, prompt structures, and agent behaviors.

---

## Table of Contents

1. [System Overview](#1-system-overview)
2. [Trigger & Entry Points](#2-trigger--entry-points)
3. [Input Resolution & Authentication](#3-input-resolution--authentication)
4. [Diff & Changed Files Extraction](#4-diff--changed-files-extraction)
5. [Code Graph — Deterministic Context Analysis](#5-code-graph--deterministic-context-analysis)
6. [Multi-Repo Context (Optional)](#6-multi-repo-context-optional)
7. [Developer Rules & File Filtering](#7-developer-rules--file-filtering)
8. [PR Memory — Previous Findings](#8-pr-memory--previous-findings)
9. [Chunking — Token-Aware File Grouping](#9-chunking--token-aware-file-grouping)
10. [Multi-Agent Orchestration](#10-multi-agent-orchestration)
11. [Prompt Architecture — What Goes Into the LLM](#11-prompt-architecture--what-goes-into-the-llm)
12. [Agentic Tool-Call Loop](#12-agentic-tool-call-loop)
13. [Consolidation — Merging Agent Results](#13-consolidation--merging-agent-results)
14. [Post-Processing & Deduplication](#14-post-processing--deduplication)
15. [GitHub Posting](#15-github-posting)
16. [Cost Tracking](#16-cost-tracking)
17. [All Limits & Budgets Reference](#17-all-limits--budgets-reference)

---

## 1. System Overview

The AI Code Reviewer is a Go-based system that performs automated code review on GitHub Pull Requests. It operates in two modes:

| Mode | Entry Point | Trigger |
|------|-------------|---------|
| **GitHub Action (CLI)** | `cmd/cli/main.go` | PR `opened` / `synchronize` via Actions workflow |
| **SaaS (Webhook)** | `main.go` | PR webhook → HTTP server → background goroutine |

### High-Level Pipeline

```
PR Opened/Updated
       │
       ▼
┌──────────────┐
│  Diff + Files │  ← git diff, file content, repo tree
└──────┬───────┘
       │
       ▼
┌──────────────┐
│  Code Graph   │  ← AST parsing, dependency resolution, runtime slicing
└──────┬───────┘
       │
       ▼
┌──────────────┐
│  Chunking     │  ← Union-Find graph-aware grouping (180K token budget)
└──────┬───────┘
       │
       ▼                    ┌─────────────────────┐
┌──────────────┐  ──────►   │  🐛 Correctness     │
│  Orchestrator │  ──────►   │  🔐 Security        │  (parallel)
└──────┬───────┘  ──────►   │  🏗️ Structure        │
       │                    └─────────┬───────────┘
       │                              │
       ▼                              ▼
┌──────────────┐            ┌─────────────────────┐
│ Consolidation │  ◄─────── │  Agent Results       │
└──────┬───────┘            └─────────────────────┘
       │
       ▼
┌──────────────┐
│  Dedup + Post │  ← Memory dedup → GitHub inline comments
└──────────────┘
```

---

## 2. Trigger & Entry Points

### GitHub Action Mode (Primary)

The `action.yml` defines the composite action. When a PR is opened or receives new commits:

1. **Setup Go** — `go 1.24` with module caching
2. **Cache Code Graph** — `.ai-reviewer/` dir is cached per repo + base SHA
3. **Resolve API Keys** — Multi-level: `input → org secret → env var`
4. **Build Binary** — `go build -o ai-reviewer cmd/cli/main.go`
5. **Fetch Git History** — Handles shallow clones by fetching merge base
6. **Execute** — Runs the binary with flags: `--llm-provider`, `--pr-number`, `--repo`, `--sha`, `--base`, `--head`, `--app-id`, `--review-rules`

### SaaS Webhook Mode

The `main.go` server listens for GitHub webhooks:

1. Validates webhook signature (`X-Hub-Signature-256`)
2. Parses the `PullRequestEvent`
3. On `opened` or `synchronize` → launches `processPR()` in a background goroutine
4. **Conflict Detection**: Checks if `.github/workflows/ai-review.yml` exists → skips SaaS review to avoid duplicates with Action mode

---

## 3. Input Resolution & Authentication

### CLI Mode Resolution Chain

```
Flag (--api-key) → Env (GEMINI_API_KEY) → Env (OPENAI_API_KEY) → Env (ANTHROPIC_API_KEY)
```

### Supported LLM Providers

| Provider | Env Variable | Model |
|----------|-------------|-------|
| Gemini (default) | `GEMINI_API_KEY` | `gemini-3.1-pro-preview-customtools` |
| OpenAI | `OPENAI_API_KEY` | Configurable |
| Claude | `ANTHROPIC_API_KEY` | Configurable |

### GitHub Authentication

- **Standard Token**: `GITHUB_TOKEN` — basic PR operations
- **GitHub App**: `APP_ID` + `APP_PRIVATE_KEY` — enables multi-repo context and richer API access
- App credentials trigger installation client creation via `action.NewGitHubAppClient()`

---

## 4. Diff & Changed Files Extraction

### CLI Mode (Local Git)

```go
diff = action.GetDiff(baseRef, headRef)        // git diff base..head
changedFiles = action.GetChangedFiles(base, head) // list of paths
// For each file: read content from disk
```

### SaaS Mode (GitHub API)

```go
diff = internalGH.FetchPRDiff(ctx, client, owner, repo, prNumber)
changedFiles = internalGH.GetChangedFilesContent(ctx, client, owner, repo, prNumber, commitSHA)
```

### PR Context Collection

The system collects PR metadata for LLM context:

```go
PRContext {
    Title          string    // PR title
    Body           string    // PR description (truncated to 1000 chars in prompt)
    CommitMessages []string  // Last 10 commit messages (each truncated to 500 chars)
}
```

### Repo Structure

Fetched via `GetRepoTree()` — a recursive tree listing of the repository for architecture-level context. Used primarily by the **Structure** agent.

---

## 5. Code Graph — Deterministic Context Analysis

The Code Graph is the system's **static analysis engine** that provides deterministic dependency context to the LLM. It works in two phases:

### Phase 1: Graph Building (Persistence Layer)

```
┌─────────────────────────────────────┐
│ Is .ai-reviewer/graph.json cached?  │
│                                     │
│  YES + current SHA → Cache hit ✅    │
│  YES + stale SHA → Delta update 🔄  │
│  NO → Full build from scratch 🔨    │
└─────────────────────────────────────┘
```

- **Full Build** (`codegraph.BuildFull`): Parses ALL files in the repo using Tree-sitter AST parsers. Extracts definitions, references, imports, and builds file-level edges.
- **Delta Update** (`codegraph.DeltaUpdate`): Only re-parses the changed files and updates the graph incrementally.
- **Graph Cache**: Stored to `.ai-reviewer/graph.json`, cached in GitHub Actions via `actions/cache@v4` keyed by `code-graph-{repo}-{base_sha}`.

### Phase 2: Context Extraction

The Code Graph Service (`codegraph.Service`) executes the context extraction:

#### Path A — Runtime Slice (Fast Path, when graph exists)

```go
view = graph.BuildRuntimeGraphView(diff, changedFiles, depth=2)
result["_codegraph/runtime_slice"] = FormatRuntimeGraphView(view)
```

Uses the pre-built graph to find symbols touched by the diff, then slices the graph to 2-hop depth to get relevant dependencies.

#### Path B — Scan-Based Discovery (Fallback)

1. **Extract references** from each changed file using Tree-sitter AST parsing
2. **Filter builtins** (language-specific built-in symbols are excluded)
3. **Find definitions** for each referenced symbol by scanning the codebase (concurrent, semaphore=10)
4. **Extract definition snippets** — only the matching definition, not the full file
5. **Build dependency summaries** — compact text descriptions per dependency file
6. **Build Context Graph** — a relationship graph showing `[file] --calls--> [file] (via: Symbol)`
7. **Build Data Flow** — traces data flow chains from changed files through dependencies (1-hop extension)

### Context Graph Output Format

```
CONTEXT GRAPH
- Nodes: 5 | Edges: 8
[agents.correctness] --calls--> [agents.specs] (via: BuildSpecialistSystemPrompt)
[orchestrator.orchestrator] --uses_type--> [agents.types] (via: AgentConfig)

DATA FLOW
- orchestrator.orchestrator -> agents.correctness via BuildSpecialistSystemPrompt
- agents.correctness -> agents.specs -> agents.prompts
```

### Limits

| Limit | Value | Source |
|-------|-------|--------|
| Max dependency context total | **12,000 chars** | `MaxDepContextChars` |
| Max symbols per file summary | **8** | `MaxSymbolsPerSummary` |
| Max snippets per dependency file | **6** | `MaxSnippetsPerFile` |
| Max behavior signals per summary | **4** | `MaxBehaviorSignals` |
| Max symbols shown per graph edge | **4** | `MaxGraphEdgeSymbols` |
| Max data flow lines | **12** | `MaxDataFlowLines` |
| Symbol snippet max length | **3,000 chars** | `symbolSnippet()` |

### Slim Dependencies Index

For agents that need to be tool-driven (Correctness, Security), the full dependency contents are replaced with a **slim index**:

```
Available dependencies (use get_symbol_definition tool to inspect):
- auth/middleware.go: AuthMiddleware, ValidateToken, RequireAdmin
- models/user.go: User, UserRole, CreateUserRequest
```

This is built by `codegraph.BuildSlimDependencyIndex()` — it strips code snippets (~2000 chars/file) down to file paths + symbol names (~50 chars/file), encouraging agents to use tool calls for actual code inspection.

---

## 6. Multi-Repo Context (Optional)

Activated when a **GitHub App token** has access to multiple repositories. This feature enables cross-repository code review.

### Pipeline

```
1. Check multi-repo availability (App token with >1 repo access)
       │
2. Discover accessible repos → filter out current repo
       │
3. Lightweight Filter: Extract local signals from code graph
   - Project name, import paths, package names, targets
   - Match against remote repo manifests
       │
4. Identify relevant repos (manifest-level match)
       │
5. Shallow clone relevant repos (WorkerPool, concurrency=5)
       │
6. Build RemoteRepoGraph for each (AST-based, same as local graph)
       │
7. Match repos: cross-reference local graph signals with remote graphs
       │
8. Format match summary → injected into agent prompts + tools
```

### Multi-Repo Tools Available to Agents

| Tool | Purpose |
|------|---------|
| `list_cross_repo_matches` | List all matched peer repositories |
| `resolve_repo_symbol` | Find where a symbol is defined in a remote repo |
| `get_repo_callers` | Find call-sites in a remote repo that reference a symbol |
| `fetch_repo_snippet` | Fetch lines of code from a remote repo |
| `search_repo_graph` | Search the graph of a remote repo for definitions/imports |

---

## 7. Developer Rules & File Filtering

### Developer Rules (`review_rules` input)

YAML-format custom rules defined by maintainers:

```yaml
instructions:
  - "Focus on SQL injection in data access layer"
  - "We use custom auth middleware, don't flag missing auth on handlers"
ignore:
  - "*.test.go"
  - "vendor/**"
  - "generated/**"
focus:
  - "security"
  - "performance"
```

Processing:
1. **Parse** via `rules.Parse(yaml)` — validates structure
2. **Sanitize** via `rules.Sanitize()` — strips unsafe content
3. **Apply ignore filter** — removes matching files from `changedFiles` before any LLM analysis
4. **Inject into prompt** via `rules.FormatForPrompt()` — added to the dynamic prompt section

### File Filtering (Two Layers)

#### Layer 1: Orchestrator Filter (`orchestrator.go`)

Removes non-reviewable files before agent processing:
- Skip by extension: `.txt`, `.rst`, `.adoc`, `.gitignore`, `.dockerignore`, `.png`, `.jpg`, `.gif`, `.svg`, `.ico`
- Skip by name: `license`, `changelog`, `readme`, `notice`, `copying`

#### Layer 2: LLM Filter (`llm.go`)

Broader filter applied during prompt construction:
- Documentation: `.md`, `.txt`, `.rst`, `.adoc`, `readme`, `changelog`, `contributing`, etc.
- Config/metadata: `.gitignore`, `.editorconfig`, `tsconfig.json`, `package.json`, `go.mod`, `go.sum`, `requirements.txt`, etc.
- Ignored directories: `node_modules`, `vendor`, `dist`, `build`, `bin`, `.git`, `.ai-reviewer`, `.idea`, `.vscode`

---

## 8. PR Memory — Previous Findings

PR Memory enables cross-commit deduplication within a PR's lifecycle.

### How It Works

```
┌─────────────────────────────────────┐
│ 1. Fetch ALL existing PR comments   │
│    from GitHub API (paginated)      │
│    - Review comments (inline)       │
│    - Issue comments (fallback)      │
└─────────────┬───────────────────────┘
              │
              ▼
┌─────────────────────────────────────┐
│ 2. Parse hidden HTML markers        │
│    <!-- ai-reviewer:v1              │
│     file=base64(path)              │
│     line=42                         │
│     severity=critical               │
│     layer=bug                       │
│     hash=abc123def456 -->           │
└─────────────┬───────────────────────┘
              │
              ▼
┌─────────────────────────────────────┐
│ 3. Build PreviousFinding list       │
│    with: file, line, severity,      │
│    layer, hash, commentID           │
└─────────────────────────────────────┘
```

### Fingerprint Algorithm

```go
normalized = lowercase(trim(message))
normalized = removeSpaces(removeNewlines(normalized))
data = file + "|" + layer + "|" + normalized
hash = SHA256(data)[:12]  // 12 hex chars
```

### Deduplication Rules

| Check | Condition | Action |
|-------|-----------|--------|
| **Exact fingerprint match** | Same hash AND line within ±10 | Skip (duplicate) |
| **Proximity match** | Same file + layer + line within ±3 + message similarity | Skip (duplicate) |
| **Message similarity** | First 20 chars match, or one contains the other | Confirms proximity match |

### LLM Context Integration

Previous findings are injected into the **dynamic prompt** so agents can:
- Avoid re-reporting known issues
- Detect if previous issues were fixed → output `resolutions` with status `resolved`/`unresolved`

---

## 9. Chunking — Token-Aware File Grouping

Chunking splits large PRs into manageable pieces for separate LLM reviews.

### Algorithm

```
1. Fast path: If total tokens ≤ budget → single chunk (no splitting)
       │
2. Build connected components using Union-Find:
   a. Union files connected by code graph edges (both files must be changed)
   b. Union files in the same directory (secondary grouping)
       │
3. Pack components into chunks by budget (first-fit):
   - If a component fits → add to current chunk
   - If it would exceed budget → flush current, start new chunk
   - If a single component exceeds budget → break it into sub-chunks
   - If a single FILE exceeds budget → it gets its own chunk
       │
4. Post-processing per chunk:
   - Assign index/total (1-based)
   - Extract per-chunk diff (split the unified diff by file headers)
   - Compute cross-refs (files in OTHER chunks this chunk depends on)
```

### Token Estimation

```go
tokens ≈ len(content) / 4  // 4 chars ≈ 1 token
```

### Key Constant

```go
DefaultTokenBudget = 180_000  // per chunk
```

### Cross-Chunk References

For each chunk, the system computes which files in OTHER chunks are dependencies:
- If chunk A's file imports/calls a file in chunk B → that file is listed as a cross-ref
- Cross-refs are reported to the LLM so it knows about related code outside its current view

---

## 10. Multi-Agent Orchestration

The orchestrator spawns 3 specialist agents per chunk in parallel.

### Agent Roles

| Agent | Type | Focus | Receives |
|-------|------|-------|----------|
| 🐛 **Correctness** | `AgentCorrectness` | Bugs, logic errors, data integrity, edge cases, resource leaks, performance | Slim deps, NO repo structure |
| 🔐 **Security** | `AgentSecurity` | Vulnerabilities, auth, injection, secrets, trust boundaries | Slim deps, NO repo structure |
| 🏗️ **Structure** | `AgentStructure` | Architecture, patterns, consistency, breaking changes, duplication | NO deps (uses tools), FULL repo structure |

### Differentiated Context Strategy

```
                    ┌──────────────────────────────────────────────┐
                    │  Orchestrator prepares AgentConfig per agent  │
                    └──────────┬───────────────────────────────────┘
                               │
        ┌──────────────────────┼──────────────────────┐
        ▼                      ▼                      ▼
  ┌──────────────┐     ┌──────────────┐      ┌──────────────┐
  │ Correctness   │     │ Security      │      │ Structure     │
  │               │     │               │      │               │
  │ ✅ Slim deps   │     │ ✅ Slim deps   │      │ ❌ No deps     │
  │ ❌ No repo tree│     │ ❌ No repo tree│      │ ✅ Repo tree   │
  │ ✅ Tools       │     │ ✅ Tools       │      │ ✅ Tools       │
  └──────────────┘     └──────────────┘      └──────────────┘
```

**Rationale**: Correctness/Security get a slim dependency index (~50 chars/file instead of ~2000), forcing them to use `get_symbol_definition` for actual code. This reduces attention dilution. Structure gets the full repo tree because it needs to understand project architecture.

### Execution Flow

```go
// 1. Filter non-reviewable files
reviewableFiles = filterReviewableFiles(chunkFiles)

// 2. Build slim dependency index
slimDeps = codegraph.BuildSlimDependencyIndex(dependencies)

// 3. Build per-agent configs
configs = []AgentConfig{
    CorrectnessAgent(shared + slimDeps),
    SecurityAgent(shared + slimDeps),
    StructureAgent(shared + repoStructure),
}

// 4. Launch 3 goroutines in parallel
for each config:
    go func() {
        toolExecutor = NewToolExecutor(repoPath, graph)
        result = RunAgentReview(ctx, provider, config, toolExecutor)
    }()

// 5. Wait for all 3 to complete
wg.Wait()

// 6. LLM-based consolidation
consolidated = RunConsolidation(ctx, provider, results, diff, maxComments=15)
```

### Global Context Caching (Multi-Chunk Optimization)

When there are multiple chunks, the system creates **global caches** for each agent type before processing any chunks:

```go
// For each agent type: Correctness, Security, Structure
staticContext = BuildGlobalStaticContext(agentConfig)
cacheID = provider.CreateCache(ctx, systemPrompt, staticContext, tools)
// This cache is reused across all chunks for the same agent type
```

Cache is created only when context exceeds **135,000 chars (~33,000 tokens)** — Gemini API requires minimum 32,768 tokens for context caching.

---

## 11. Prompt Architecture — What Goes Into the LLM

Each agent receives a carefully structured prompt with two parts:

### Part 1: System Prompt (Agent Identity)

```
┌─────────────────────────────────────────────────────┐
│  Agent-Specific System Prompt                        │
│  (CorrectnessSystemPrompt / SecuritySystemPrompt /   │
│   StructureSystemPrompt)                             │
│                                                      │
│  + Shared Mandatory Review Rules                     │
│    - Rule 1: Review only added live code              │
│    - Rule 2: Diff semantics ([OLD_REMOVED_CODE] etc) │
│    - Rule 3: Precision (no praise, concrete fixes)   │
│    - Rule 4: Severity definitions                    │
│    - Rule 5: PR Context usage                        │
│    - Rule 6: Verify with tools                       │
│    - Rule 7: Output JSON schema                      │
│    - Rule 8: Dedup                                   │
│                                                      │
│  + Multi-Repo Block (if cross-repo matches exist)    │
└─────────────────────────────────────────────────────┘
```

### Part 2: User Prompt (Static Context + Dynamic Prompt)

The user prompt is split into **Static Context** (cacheable) and **Dynamic Prompt** (per-chunk):

#### Static Context (Cached)

```
═══════════════════════════════════════════
GLOBAL CONTEXT: Code Graph Dependencies
═══════════════════════════════════════════
(Slim dependency index OR full deps, depending on agent)

═══════════════════════════════════════════
GLOBAL CONTEXT: Repository Structure
═══════════════════════════════════════════
(Full repo tree — Structure agent only)
```

#### Dynamic Prompt (Per-Chunk)

```
═══════════════════════════════════════════
TASK CONTEXT: Previous Review Findings
═══════════════════════════════════════════
- [ID: 123] [bug] file.go:42: Message...  (truncated to 200 chars)

=== CHUNK 2/3 ===
Files in other chunks: path/to/related.go

═══════════════════════════════════════════
TASK CONTEXT: PR Intent
═══════════════════════════════════════════
Title: Fix authentication middleware
Description: ... (truncated to 1000 chars)
Recent Commits:
  1. Fix JWT validation
  2. Add token refresh

═══════════════════════════════════════════
Developer Rules (if present)
═══════════════════════════════════════════
Instructions: ...
Focus areas: ...

═══════════════════════════════════════════
ACTIONABLE TARGET: Changed Code Files
═══════════════════════════════════════════
INSTRUCTION: Analyze ONLY the lines marked with '+' in the DIFF HUNKS below.

═══ FILE: internal/auth/middleware.go ═══
[Showing imports + ±40 lines around each change]
[Use get_file_content tool to inspect other sections if needed]

1: package auth
2: import (
   ...
)
... (lines 5-35 omitted) ...

36: func ValidateToken(token string) (*Claims, error) {
37:     ...
42:     if token == "" {   ← changed line
    ...

────────────────────────────────────────
DIFF HUNKS FOR: internal/auth/middleware.go (ANALYZE THESE CHANGES)
────────────────────────────────────────
LEGEND:
  [OLD_REMOVED_CODE] = This code was DELETED.
  [NEW_LIVE_CODE]    = This code was ADDED.

/* Change Block 1:
@@ -40,5 +40,8 @@
[OLD_REMOVED_CODE] if token == nil {
[NEW_LIVE_CODE]    if token == "" {
[NEW_LIVE_CODE]        return nil, ErrEmptyToken
                   }
*/

═══════════════════════════════════════════
BEGIN ANALYSIS NOW.
═══════════════════════════════════════════
REMINDER: Your response MUST be a valid JSON object with "thinking",
"summary", "comments", and "resolutions" keys.
```

### Safe Diff Formatting

Raw diff `+`/`-` prefixes are replaced with semantic tags to prevent LLM confusion:

```
- deleted line    →  [OLD_REMOVED_CODE] deleted line
+ added line      →  [NEW_LIVE_CODE]    added line
  context line    →                     context line
```

### Focused Context Window

For each changed file, the system shows:
- **Import/package header** (always included)
- **±40 lines** around each diff hunk (from `extractDiffWithContext`)
- **"... (lines X-Y omitted) ..."** markers between windows
- A prompt telling the agent to use `get_file_content` for other sections

### Response Schema (Enforced on Gemini)

```json
{
  "thinking": "Trace data flows, verify intent...",  // Required
  "summary": "Found 2 critical, 1 warning issue(s)",  // Required
  "comments": [                                        // Required
    {
      "file": "path/to/file.ext",
      "line": 42,
      "severity": "critical|warning|info",
      "layer": "bug|performance|security|architecture|lint",
      "message": "Problem. Fix."
    }
  ],
  "resolutions": [                                     // Optional
    {
      "comment_id": 12345,
      "status": "resolved|unresolved",
      "reason": "Why"
    }
  ]
}
```

---

## 12. Agentic Tool-Call Loop

Each agent runs in an **agentic loop** that supports multi-turn tool calls:

```
┌──────────────────────────────────────────────────────┐
│  Iteration 0                                          │
│  Send: System Prompt + User Prompt (static + dynamic) │
│  Recv: Either tool calls OR final JSON response       │
└──────────┬───────────────────────────────────────────┘
           │
           ▼
     ┌─────────────────┐     YES     ┌──────────────────┐
     │ Has tool calls?  │───────────▶│ Execute tools     │
     └─────────────────┘             │ (parallel)        │
           │ NO                      └────────┬─────────┘
           │                                  │
           ▼                                  │
     ┌─────────────────┐                      │
     │ Parse JSON       │◀────────────────────┘
     │ Return result    │     (append results to conversation,
     └─────────────────┘      loop back for next iteration)
```

### Tool Declarations

| Tool | Description | Output Limit |
|------|-------------|-------------|
| `get_symbol_definition` | Find source code of a function/type/struct by name | **5,000 chars** max snippet |
| `get_file_content` | Read a file from the repo (supports windowed line ranges) | **40,000 chars** max (full file) |
| `get_callers` | Find all references/call-sites for a symbol | Deduplicated, sorted |
| `search_codebase` | `git grep -n -I --max-count=5 <query>` | **30 lines** max (5 per file) |
| `list_cross_repo_matches` | List multi-repo match summary | — |
| `resolve_repo_symbol` | Find symbol definition in a remote repo | — |
| `get_repo_callers` | Find call-sites in a remote repo | — |
| `fetch_repo_snippet` | Fetch lines from a remote repo file | — |
| `search_repo_graph` | Search remote repo graph for definitions/imports | **50 results** max |

### Loop Prevention

- **Duplicate call detection**: `sync.Map` tracks `tool_name + JSON(args)` — identical calls return an error message instead of re-executing
- **Max iterations**: **10** — after 10 tool-call rounds, the loop terminates with an error

### Empty Response Handling

- On first empty response → append a "nudge" prompt asking for JSON analysis → retry
- On second empty response → return "No findings" result

### Context Caching

```
IF static context > 135,000 chars (~33,000 tokens):
    Create Gemini Context Cache (system prompt + static context + tool declarations)
    All subsequent requests reference the cache ID
    
IF caching fails or context is small:
    Inline static context into the dynamic prompt
```

---

## 13. Consolidation — Merging Agent Results

After all 3 agents complete, their results are consolidated into a single review.

### LLM-Based Consolidation (Primary)

Uses **Gemini Flash** (`gemini-2.5-flash`) for fast, cheap consolidation:

1. Build a consolidation prompt containing:
   - The original diff (in safe format with `[NEW_LIVE_CODE]` tags)
   - All findings from each agent with file, line, severity, layer, message
2. System prompt instructs the consolidator to:
   - **Semantically deduplicate** — merge conceptually same issues from different agents
   - **Group lines** — merge same issue on multiple lines into one comment: `"Lines 42, 45, 48: ..."`
   - **Remove false positives** — drop vague/speculative comments
   - **Validate against diff** — drop comments where line number doesn't exist in `[NEW_LIVE_CODE]` blocks
   - **Cap at 15 comments** — prioritize `critical > warning > info`
   - **Merge resolutions** — if multiple agents resolve same comment_id, prioritize "resolved"
3. Temperature: **0.0** (deterministic)

### Deterministic Consolidation (Fallback)

Used when LLM consolidation fails (API error, empty response, JSON parse error):

1. **Pass 1: Exact dedup** — by `(file, line, SHA256(message)[:12])` — keep higher severity on conflicts
2. **Pass 2: Cross-line dedup** — by `(file, SHA256(message)[:12])` across lines → merge into "Lines X, Y, Z: ..." 
3. **Sort** — `critical > warning > info`, then by file, then by line
4. **Cap** — first 15 comments after sorting
5. **Merge resolutions** — deduplicate by comment_id, prioritize "resolved"

### Cross-Chunk Consolidation

When a PR spans multiple chunks, each chunk produces its own consolidated result. These are then merged:

```go
result = llm.ConsolidateResults(results)  // merge all chunk results
```

---

## 14. Post-Processing & Deduplication

### Memory-Based Deduplication

Before posting, new findings are checked against previous findings from earlier commits:

```go
result.Comments, skipped = memory.Deduplicate(result.Comments, previousFindings)
```

This prevents re-posting the same comment when a PR is updated with new commits.

### Comment Formatting

Each comment is formatted with an emoji prefix based on the layer:

| Layer | Prefix |
|-------|--------|
| `security` | 🔐 Security |
| `bug` | 🐛 Bug |
| `performance` | ⚡ Performance |
| `lint` | 🧹 Lint |
| `architecture` | 🏗️ Architecture |

---

## 15. GitHub Posting

### CLI Mode — Pull Request Review

```go
ghClient.PostReview(ctx, prNumber, result, commitSHA, diff)
```

Posts as a **GitHub Pull Request Review** with inline comments attached to specific diff lines.

### SaaS Mode — Individual Comments

```go
// For each comment:
ghComment = &github.PullRequestComment{
    Body:     "🔐 Security\nSQL injection via...",
    Path:     "path/to/file.go",
    Line:     42,
    Side:     "RIGHT",  // always the new file side
    CommitID: commitSHA,
}
internalGH.PostComment(ctx, client, owner, repo, prNumber, ghComment)
```

### Fallback for Invalid Lines

If a comment targets a line **outside the diff range** (GitHub returns 422):
1. Retry as a **general PR comment** (not inline)
2. Format: `"failed to comment on file.go:L42 (likely outside diff):\n\n🐛 Bug\n..."`

### Check Run Integration (SaaS Mode)

1. **Create Check Run** at start → shows "in progress" on the PR
2. **Update Check Run** on completion → "success" with summary
3. On failure → "failure" with error message

---

## 16. Cost Tracking

The system wraps each LLM provider with cost tracking:

```go
provider, costLedger = llm.WrapWithCostTracking(rawProvider)
```

At the end of the run:

```go
costLedger.PrintSummary("💰 [LLM Cost]")
```

Tracks:
- Input tokens
- Output tokens
- Cache read/write tokens (Gemini context caching)
- Estimated dollar cost

---

## 17. All Limits & Budgets Reference

### Token & Context Budgets

| Limit | Value | Location |
|-------|-------|----------|
| Chunk token budget | **180,000 tokens** | `chunker.DefaultTokenBudget` |
| Max context chars (SaaS prompt) | **720,000 chars** (~180K tokens) | `llm.MaxContextChars` |
| Context cache minimum | **135,000 chars** (~33K tokens) | `agent_loop.go` |
| Token estimation | **4 chars = 1 token** | `chunker.EstimateTokens` |

### Truncation Limits

| What | Limit | Location |
|------|-------|----------|
| PR body in prompt | **1,000 chars** | `buildDynamicPrompt()` |
| Commit messages | **500 chars** each, last **10** | `buildDynamicPrompt()` |
| Previous finding messages | **200 chars** each | `buildDynamicPrompt()` |
| Focused file context window | **±40 lines** around each hunk | `buildDynamicPrompt()` |
| SaaS focused context window | **±50 lines** around each hunk | `buildPrompt()` |
| Fallback: no diff sections | **first 100 lines** | `buildDynamicPrompt()` |
| Safe UTF-8 truncation | Character-boundary safe | `truncateUTF8()` |

### Tool Output Limits

| Tool | Limit | Location |
|------|-------|----------|
| `get_symbol_definition` snippet | **5,000 chars** | `tools.go` |
| `get_file_content` full file | **40,000 chars** | `tools.go` |
| `search_codebase` results | **30 lines**, 5 matches per file | `tools.go` |
| `search_repo_graph` results | **50 results** max | `tools.go` |

### Agent Limits

| Limit | Value | Location |
|-------|-------|----------|
| Max tool iterations per agent | **10** | `agent_loop.maxToolIterations` |
| Max comments after consolidation | **15** | `agents.DefaultMaxComments` |
| Agent temperature | **0.0** | `agent_loop.go`, `consolidator.go` |
| LLM model (main review) | `gemini-3.1-pro-preview-customtools` | `llm.go` |
| LLM model (consolidation) | `gemini-2.5-flash` | `llm.go` |

### Code Graph Limits

| Limit | Value | Location |
|-------|-------|----------|
| Max dependency context total | **12,000 chars** | `codegraph.MaxDepContextChars` |
| Max symbols per file summary | **8** | `codegraph.MaxSymbolsPerSummary` |
| Max snippets per dependency | **6** | `codegraph.MaxSnippetsPerFile` |
| Max behavior signals | **4** | `codegraph.MaxBehaviorSignals` |
| Max graph edge symbols | **4** | `codegraph.MaxGraphEdgeSymbols` |
| Max data flow lines | **12** | `codegraph.MaxDataFlowLines` |
| Runtime slice depth | **2 hops** | `service.go` |

### Memory / Dedup Limits

| Limit | Value | Location |
|-------|-------|----------|
| Fingerprint hash length | **12 hex chars** (SHA-256) | `memory.Fingerprint()` |
| Exact match line tolerance | **±10 lines** | `memory.Deduplicate()` |
| Proximity match line tolerance | **±3 lines** | `memory.Deduplicate()` |
| Message similarity prefix | **20 chars** | `memory.Deduplicate()` |

### Multi-Repo Limits

| Limit | Value | Location |
|-------|-------|----------|
| Worker pool concurrency | **5** | `multirepo.NewWorkerPool(5)` |
| Remote fetch concurrency | **5** | `remotefetch.NewFetcher` |
| Remote fetch max lines | **200** | `remotefetch.NewFetcher` |
| Remote fetch max bytes | **500,000** | `remotefetch.NewFetcher` |

### Priority-Based Context Dropping

When the total context exceeds the budget, items are dropped in this order (lowest priority first):

```
1. Repo structure    → truncated or omitted entirely
2. Large dep files   → dropped (smallest deps kept first)
3. Changed files     → NEVER dropped (highest priority)
```
