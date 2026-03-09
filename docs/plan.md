# Multi-Repo PR Review Plan: Automatic Cross-Repo Enrichment

> Updated: 2026-03-06
> Status: Refined implementation plan
> Decision: Build fresh code graphs at runtime and automatically enrich reviews with cross-repo context when multiple repos are accessible — zero configuration from developers

---

## 1. Goal

Build a multi-repo review capability where:

- the review runs on the GitHub Actions runner of the PR repository
- the reviewer implementation stays in the private reviewer repository
- dependent repositories are private
- Gemini is the LLM provider
- **the current single-repo review behavior remains completely unchanged**
- multi-repo review is an **automatic** enrichment layer — not a replacement, not opt-in per PR
- developers do **nothing** different — no config, no annotations, no registry, no manifest
- if a GitHub App installation has access to multiple repos, cross-repo review happens transparently
- if a GitHub App installation has access to only one repo (or no App token exists), the review behaves identically to today
- code graphs are built fresh on every PR run
- no graph is committed into any application repo
- no graph storage or caching is part of the initial phases

---

## 2. Key Principle: Zero Developer Friction

The fundamental design principle is:

> **Developers should never need to know about multi-repo review.** It should happen automatically when the system detects accessible sibling repositories. When there is only one repo, the system works exactly as it does today.

This means:

- ❌ No `multi_repo: true` flag developers need to set
- ❌ No service registry or contract files to maintain
- ❌ No explicit repo lists in workflow files
- ❌ No special action inputs or PR labels
- ✅ The system auto-detects whether multi-repo context is available
- ✅ If the GitHub App installation sees multiple repos → cross-repo enrichment activates
- ✅ If the GitHub App installation sees one repo (or token is missing) → single-repo review as today
- ✅ All fallback paths silently degrade to single-repo review

---

## 3. Scope of This Version

This version deliberately does **not** solve graph storage, artifact distribution, or cache reuse.

The design optimizes for **correctness of architecture** and **deterministic behavior**, not CI speed.

The runtime model is:

1. Preserve the existing single-repo review path as the baseline
2. Discover peer repos at runtime when GitHub App access is available
3. Build lightweight graphs at runtime for discovered repos
4. Select candidate repos deterministically at runtime
5. Fetch exact snippets at runtime
6. Discard everything when the job ends

---

## 4. How It Works Today (Unchanged)

The current single-repo pipeline that **must remain untouched**:

```
PR Opened → GitHub Actions Trigger
  → CLI Entrypoint (cmd/cli/main.go)
    → Fetch diff, changed files, repo structure
    → Build CodeGraph (codegraph.NewService → BuildFull/DeltaUpdate)
    → Get dependency context (cgService.GetContext)
    → Chunk files (chunker.GroupFiles)
    → For each chunk:
      → Orchestrator spawns 3 parallel agents (Correctness, Security, Structure)
      → Each agent runs agentic loop with tool calls (llm.RunAgentReview)
      → Results consolidated (llm.RunConsolidation)
    → Cross-chunk consolidation (llm.ConsolidateResults)
    → Post comments to GitHub PR
```

**None of these steps change.** Multi-repo is layered on top, injecting additional context into the existing pipeline only when available.

---

## 5. Hard Constraints

1. The review must execute inside GitHub Actions.
2. PR code stays on the PR runner — never sent to external services except Gemini.
3. The reviewer repo (this repo) is private.
4. Cross-repo access must use GitHub-native auth (GitHub App installation token).
5. We do not depend on OpenAPI/proto specs being present or correct.
6. We do not depend on developers maintaining service maps or registries.
7. We avoid sending whole dependent repos to Gemini.
8. Existing single-repo reviews must continue working even if multi-repo discovery, auth, or graph build fails.
9. **No new inputs or flags required in the consuming repo's workflow file.** The GitHub App token is already available via the installation.

---

## 6. Repository Universe (Automatic Discovery)

Repo discovery is fully automatic and deterministic at runtime.

### 6.1 Source of truth

The system enumerates repositories from the GitHub App installation used by the PR repository.

**Selection rules (automatic):**

| Rule | Description |
|------|-------------|
| ✅ Include | Repos accessible to the same installation token |
| ✅ Include | Private and internal repos only |
| ❌ Exclude | Archived repos |
| ❌ Exclude | Disabled repos |
| ❌ Exclude | Empty repos |
| ❌ Exclude | Forks (unless explicitly enabled later) |
| ❌ Exclude | The current PR repo itself (already analyzed locally) |

This gives a deterministic repo universe without a manual registry.

### 6.2 Detection logic

```
GitHub App Token available?
  ├─ NO  → single-repo review only (today's behavior)
  └─ YES → list installation repos
            ├─ Only 1 repo visible → single-repo review only
            └─ 2+ repos visible → activate cross-repo enrichment
```

Developers do **nothing** to trigger this. The presence of a GitHub App with multi-repo access is sufficient.

---

## 7. Backward Compatibility

### 7.1 Existing behavior that stays unchanged

The current single-repo flow continues to work exactly as it does today:

- Local diff collection via `action.GetDiff()`
- Local file loading via `action.GetFileContent()`
- Local code graph build via `codegraph.BuildFull()` / `codegraph.DeltaUpdate()`
- Context generation via `cgService.GetContext()`
- Chunking via `chunker.GroupFiles()`
- 3-agent parallel review via `orchestrator.ReviewChunk()`
- Agent tool calls via `llm.NewToolExecutor()`
- Consolidation via `llm.RunConsolidation()` and `llm.ConsolidateResults()`
- Comment posting via `ghClient.PostReview()`

### 7.2 Fallback rules

If **any** of these happen:

- GitHub App token is missing or not configured
- Installation repo listing fails (API error, rate limit, etc.)
- Only one repo visible to the installation
- Remote repo checkout fails
- Remote graph build fails
- Deterministic repo matching finds zero candidates

Then the system **must**:

- Continue the local single-repo review with no changes
- Skip cross-repo enrichment silently
- Never fail the entire review job due to multi-repo issues
- Log a brief message (e.g., `ℹ️ Multi-repo: skipped (reason)`) for debugging

### 7.3 Integration rule

The implementation must not replace existing local tools or orchestrator behavior.

It must:

- Keep all current local tools untouched for single-repo usage
- Add new multi-repo tools **alongside** them
- Inject cross-repo context only when it is non-empty
- Preserve current prompt behavior when cross-repo context is absent

---

## 8. Runtime Architecture

### 8.1 End-to-end flow

```
  PR opened / updated
        │
        ▼
  ┌─────────────────────────────────┐
  │  EXISTING SINGLE-REPO FLOW     │  ← Unchanged
  │  (diff, files, graph, context)  │
  └─────────────┬───────────────────┘
                │
        ┌───────┴──────────┐
        │ App token exists │
        │ & 2+ repos?      │
        └───────┬──────────┘
          NO    │    YES
          │     │     │
          │     │     ▼
          │     │  ┌──────────────────────────────────┐
          │     │  │ MULTI-REPO ENRICHMENT LAYER      │
          │     │  │ 1. Enumerate accessible repos    │
          │     │  │ 2. Shallow clone each → temp dir │
          │     │  │ 3. Build lightweight graph each  │
          │     │  │ 4. Extract change signals        │
          │     │  │ 5. Deterministic matching         │
          │     │  │ 6. Generate match summary        │
          │     │  └──────────────┬───────────────────┘
          │     │                 │
          ▼     ▼                 ▼
  ┌─────────────────────────────────┐
  │  REVIEW (3 Agents in Parallel)  │  ← Existing orchestrator
  │  + cross-repo context if any    │
  │  + cross-repo tools if available│
  └─────────────┬───────────────────┘
                │
                ▼
  ┌─────────────────────────────────┐
  │  CONSOLIDATION & POST           │  ← Existing consolidation
  └─────────────────────────────────┘
                │
                ▼
        Temp dirs cleaned up
```

### 8.2 Why this is the right design for now

- No merge conflicts from committed graph files
- No dependency on cache correctness
- No dependency on artifact freshness
- No dependence on developers maintaining metadata
- Deterministic repo discovery and candidate selection
- No regression to current local-only review flow

The tradeoff is longer CI time, which is acceptable for the initial phases.

---

## 9. Graph Strategy

### 9.1 Local PR repo graph (unchanged)

Already built by the existing `codegraph` package. No changes needed.

This graph includes:

- Definitions, references, imports, cross-file edges
- Exported/public symbols
- Route/provider and route/consumer signals
- Event producer/consumer signals
- gRPC service/client signals

### 9.2 Remote repo graph (new — lightweight)

For non-PR repos, build a lighter graph optimized for matching, not full review context.

The remote graph captures:

| Category | Signals extracted |
|----------|-------------------|
| **Identity** | Repo name, default branch SHA, module/import roots |
| **Exports** | Exported/public symbols with file path + line ranges |
| **HTTP** | Routes provided, routes consumed |
| **Events** | Topics produced, topics consumed |
| **gRPC** | Services, methods, messages provided and consumed |
| **Packages** | Package/module references |

Remote graph output stays in memory or temp files only for that job.

### 9.3 Build mode

Use a bounded worker pool to build remote graphs concurrently.

Behavior:

- Shallow checkout only (default branch, `--depth 1`)
- Skip binary, assets, vendor, and build output directories
- Stop graph extraction at metadata level
- Do not preload raw source into LLM context

---

## 10. Deterministic Repo Selection

Repo selection is code-based and deterministic — no LLM guessing, no manual registry.

### 10.1 Local change signals

From the PR diff and local graph, extract these signals:

- Changed exported functions, methods, types
- Changed package or module import paths
- Added/removed/renamed HTTP routes
- Changed HTTP request/response structs near handlers
- Added/removed/renamed event topics
- Changed event payload structs near publishers/handlers
- Changed gRPC service/method/message names
- Changed shared library public APIs

### 10.2 Remote repo facts

From each remote repo graph, extract these facts:

- Imported modules and packages
- Referenced external symbols
- Provided and consumed HTTP routes
- Produced and consumed events
- Provided and consumed gRPC services/methods/messages

### 10.3 Matching rules (deterministic, ordered)

| Priority | Rule | Description |
|----------|------|-------------|
| **1** | Direct module import | Remote repo imports a changed shared module/package from the PR repo |
| **2** | HTTP route match | PR changes a route and a remote repo consumes the exact method + normalized path |
| **3** | Event topic match | PR changes a produced/consumed event topic and a remote repo references the same topic |
| **4** | gRPC service/method match | PR changes a gRPC service/method/message and a remote repo references it |
| **5** | Shared symbol + package | PR changes an exported symbol and a remote repo references the same symbol within a matching namespace |
| **6** | File path / package basename correlation | Low-confidence tie-breaker only — never the sole reason for a high-severity finding |

### 10.4 Candidate threshold

A repo becomes a candidate if:

- It matches **any** of rules 1–4, **or**
- It matches rule 5 with namespace confirmation

Rule 6 alone is **never** sufficient to select a repo.

### 10.5 Deterministic output

Before any LLM call, generate a structured summary:

```text
CROSS-REPO MATCHES
- org/notification-service
  reason: exact event topic match
  topic: user.deleted
  file: internal/events/user_deleted.go:20-60

- org/billing-service
  reason: exact HTTP consumer match
  route: DELETE /api/v1/users/{id}
  file: internal/clients/user_client.go:55-120

- org/shared-auth
  reason: shared exported symbol match
  symbol: ValidateToken
  file: auth/client.go:10-42
```

This summary (not raw remote code) is what the LLM sees first.

---

## 11. Source Fetch Strategy

Remote source fetch must stay lazy and bounded.

### 11.1 Tool set

| Tool | Purpose |
|------|---------|
| `list_cross_repo_matches` | Returns deterministic candidate repos and why they matched |
| `resolve_repo_symbol` | Resolves repo + file + line range from the in-memory graph |
| `fetch_repo_snippet` | Fetches exact file lines after repo/path/range are known |
| `search_repo_graph` | Searches graph metadata (not raw source) across built remote graphs |

### 11.2 Tool behavior rules

- Existing local tools (`get_symbol_definition`, `get_callers`, `get_file_content`, `search_codebase`) remain unchanged
- New tools operate only on repos discovered during this run
- Tools never trigger blind org-wide source searches
- Tools never load full repo contents into prompt context

### 11.3 Hard limits

| Limit | Value |
|-------|-------|
| Max candidate repos sent to agents | 10 |
| Max remote snippets fetched per agent | 10 |
| Max snippet size | 250 lines |
| Max total fetched remote bytes per review | Fixed budget (configurable) |
| Deduplication | Repeated fetches across agents are deduplicated |

---

## 12. Deterministic Review Logic

The review is deterministic first, LLM second.

### 12.1 Pre-LLM stage

Before any agent runs:

1. Extract local change signals from diff + local graph
2. Build remote graph facts from discovered repos
3. Apply deterministic match rules (§10.3)
4. Rank candidates by match strength
5. Send only the top candidates + reasons into the LLM context

If no candidates exist → skip all cross-repo steps and continue with local-only review (identical to today).

### 12.2 LLM stage

The LLM then:

- Reviews the diff (as today)
- Inspects the match summary (if present)
- Requests exact remote snippets when needed (via new tools)
- Writes findings only after code-backed verification

### 12.3 Severity rule

High-severity cross-repo findings **must** require:

- A deterministic repo match (rules 1–5), **AND**
- Fetched remote snippet evidence

No high-severity finding should rely only on symbol-name coincidence.

---

## 13. Concrete Code Changes Needed

### 13.1 New runtime packages

| Package | Purpose |
|---------|---------|
| `backend/internal/multirepo` | Runtime repo discovery, temp checkout orchestration, worker pool |
| `backend/internal/reposelect` | Deterministic candidate matching rules |
| `backend/internal/remotefetch` | Exact snippet fetch for selected repos |

### 13.2 Extend code graph package (`backend/internal/codegraph`)

Enhance to emit both:

- Full local review graph (existing — no changes)
- Lightweight remote graph metadata (new)

Add extraction support for:

- Exported symbol metadata with file + line range
- Package/module identity
- Provider/consumer HTTP routes
- Producer/consumer event topics
- gRPC provider/consumer metadata

### 13.3 Extend GitHub integration (`backend/internal/github/client.go`)

Add new functions (existing functions remain unchanged):

- `ListInstallationRepos()` — enumerate repos accessible to the App installation
- `GetRepoDefaultBranch()` — get the default branch name
- `ShallowClone()` — shallow checkout into temp directory
- `FetchFileByPath()` — fetch file content by repo/path/ref for snippets

### 13.4 New LLM tools

Add alongside existing tools (existing tools are untouched):

- `list_cross_repo_matches` — returns deterministic candidate repos + match reasons
- `resolve_repo_symbol` — resolves repo + file + line range from in-memory graph
- `fetch_repo_snippet` — fetches exact file lines after repo/path/range are known
- `search_repo_graph` — searches graph metadata across built remote graphs

### 13.5 Orchestrator and prompt changes (`backend/internal/orchestrator`)

Update the agent context to **optionally** include:

- Cross-repo match summary (only when non-empty)
- Fetched remote snippet summary (only when requested by agent)

Prompt additions:

- Trust deterministic matches over guesswork
- Fetch remote code only after a concrete match exists
- Do not speculate about cross-repo impact without evidence
- **Behave exactly like today when no cross-repo context is supplied**

### 13.6 CLI changes (`backend/cmd/cli/main.go`)

Update to:

- Detect GitHub App token availability automatically
- Preserve the existing local-only path as the default (unchanged code path)
- Trigger runtime repo discovery only when App token is present and 2+ repos are visible
- Build remote graphs in temp directories
- Pass match summaries into the orchestrator only when available
- Manage remote fetch budgets
- Clean up temp directories on exit

### 13.7 Workflow changes (`action.yml`)

Minimal changes:

- The `action.yml` already has access to `GITHUB_TOKEN` — for multi-repo enrichment, a GitHub App installation token is needed
- Add an **optional** `app_id` and `app_private_key` input (or detect from environment)
- If these aren't provided, single-repo review runs as today — no error, no warning

---

## 14. Implementation Phases

### Phase 0: Compatibility Guardrails

**Goal:** Ensure the current single-repo behavior is explicitly preserved before adding any multi-repo logic.

**Deliverables:**

- [ ] Add explicit local-only execution path guard in `main.go`
- [ ] Add internal feature detection (not a dev-facing flag): `isMultiRepoAvailable()` based on token + repo count
- [ ] Add fallback behavior when cross-repo setup is unavailable
- [ ] Add regression tests proving current single-repo reviews produce identical output
- [ ] Document the fallback contract

**Success criteria:**

- Running without a GitHub App token produces **identical** behavior to today
- All existing tests pass without modification

**Estimated effort:** 2–3 days

---

### Phase 1: Runtime Repo Discovery & Lightweight Remote Graphs

**Goal:** Discover accessible repos at runtime and build remote metadata graphs, without affecting the local-only path.

**Deliverables:**

- [ ] Implement `backend/internal/multirepo` package:
  - `DiscoverRepos()` — list installation repos, apply filters (§6.1)
  - `ShallowCheckout()` — clone default branch into temp dir (`--depth 1`)
  - Worker pool with bounded concurrency
  - Cleanup helpers for temp directories
- [ ] Implement lightweight remote graph builder in `backend/internal/codegraph`:
  - `BuildRemoteGraph()` — extracts only metadata-level signals
  - Produces `RemoteRepoGraph` struct with exports, routes, events, gRPC metadata
- [ ] Extend `backend/internal/github/client.go`:
  - `ListInstallationRepos()` — enumerate accessible repos
  - `GetRepoDefaultBranch()` — get default branch name
- [ ] Add unit tests for repo filtering logic
- [ ] Add unit tests for remote graph extraction

**Success criteria:**

- Remote graphs build in parallel while local-only review still works unchanged
- Shallow clones use minimal disk/network
- All temp dirs are cleaned up after the job

**Estimated effort:** 5–7 days

---

### Phase 2: Deterministic Repo Matching

**Goal:** Select candidate repos through code-based matching only — no LLM, no registry.

**Deliverables:**

- [ ] Implement `backend/internal/reposelect` package:
  - `ExtractLocalSignals()` — extract change signals from diff + local graph (§10.1)
  - `ExtractRemoteFacts()` — extract matchable facts from remote graphs (§10.2)
  - `MatchRepos()` — apply deterministic rules in priority order (§10.3)
  - `RankCandidates()` — rank by match strength with threshold (§10.4)
  - `FormatMatchSummary()` — produce deterministic text summary (§10.5)
- [ ] Add unit tests for:
  - Route normalization and matching
  - Event topic exact-match selection
  - gRPC service/method matching
  - Exported symbol + namespace matching
  - Edge cases: similar but not matching names, empty signals, etc.
- [ ] Add integration tests with fixture repos simulating:
  - Shared library dependency
  - HTTP provider/consumer pair
  - Event producer/consumer pair
  - gRPC provider/consumer pair
  - Unrelated repo with similar symbol names (should NOT match)
  - Zero-match scenario

**Success criteria:**

- Candidate repos are explainable (each has a reason + evidence)
- No registry or developer config is required
- Unrelated repos are reliably excluded

**Estimated effort:** 5–7 days

---

### Phase 3: Additive LLM Enrichment

**Goal:** Expose cross-repo context and remote snippet tools to the existing review agents, without changing local review behavior.

**Deliverables:**

- [ ] Implement `backend/internal/remotefetch` package:
  - `FetchSnippet()` — fetch exact file lines from a known repo/path/range
  - Snippet budget tracker (max lines, max bytes)
  - Deduplication of repeated fetches
- [ ] Add new LLM tools (§11.1):
  - `list_cross_repo_matches`
  - `resolve_repo_symbol`
  - `fetch_repo_snippet`
  - `search_repo_graph`
- [ ] Update orchestrator (`orchestrator.ReviewChunk`) to:
  - Accept optional cross-repo match summary
  - Pass it to agent configs only when non-empty
  - Register new tools alongside existing local tools
- [ ] Update agent prompts to:
  - Include cross-repo context section (only when present)
  - Instruct model to trust deterministic matches
  - Instruct model to fetch evidence before high-severity cross-repo findings
  - Produce identical behavior when no cross-repo context is present
- [ ] Update `main.go` to:
  - Run multi-repo enrichment layer between graph build and review
  - Pass match summaries into the orchestrator
  - Manage snippet budget

**Success criteria:**

- No-candidate case behaves **identically** to today
- Candidate case adds verified cross-repo reasoning
- High-severity findings require snippet evidence (§12.3)

**Estimated effort:** 5–7 days

---

### Phase 4: End-to-End Integration & Workflow

**Goal:** Wire everything together in `action.yml` and validate with real multi-repo setups.

**Deliverables:**

- [ ] Update `action.yml`:
  - Add optional `app_id` and `app_private_key` inputs
  - Pass App token to CLI when available
  - No changes needed if App inputs aren't provided (single-repo as today)
- [ ] End-to-end integration tests:
  - Single-repo review (no App token) — identical to today
  - Multi-repo review with fixture repos
  - Failure scenarios: App token invalid, API rate limit, checkout failure
- [ ] Add timing logs for:
  - Repo discovery duration
  - Per-repo graph build duration
  - Matching duration
  - Total multi-repo overhead
- [ ] Verify cleanup of all temp directories

**Success criteria:**

- `action.yml` works without any App inputs (single-repo, as today)
- `action.yml` with App inputs automatically enriches with cross-repo context
- All failure paths silently degrade to single-repo review

**Estimated effort:** 3–5 days

---

### Phase 5: Stabilization & Rollout

**Goal:** Validate performance, fallback behavior, and review quality before broad enablement.

**Deliverables:**

- [ ] Performance profiling with real-world repo counts (10, 25, 50 repos)
- [ ] Tune worker pool concurrency and timeouts
- [ ] Add per-run metrics logging:
  - Number of repos discovered
  - Number of candidates matched
  - Number of snippets fetched
  - Total multi-repo overhead time
- [ ] Verify high-severity evidence enforcement (§12.3)
- [ ] Test with edge cases:
  - Very large org (100+ repos)
  - Repos with unusual structures (monorepo, polyglot, etc.)
  - Dynamic routing or reflection-based dependencies (expect low confidence)
- [ ] Rollout to selected test repos first

**Success criteria:**

- Cross-repo enrichment is reliable and does not cause timeouts
- Single-repo regressions are ruled out
- False positive rate from cross-repo findings is acceptable

**Estimated effort:** 3–5 days

---

## 15. Testing Strategy

### 15.1 Unit tests

| Test area | What to test |
|-----------|-------------|
| Fallback path | Local-only review works when multi-repo is unavailable |
| Repo discovery | Filtering rules (archived, disabled, empty, forks) |
| Remote graph | Extraction of exports, routes, events, gRPC metadata |
| Route matching | Normalization, method + path matching, path parameters |
| Event matching | Topic exact-match selection |
| gRPC matching | Service/method/message matching |
| Symbol matching | Exported symbol + namespace confirmation |
| Ranking | Candidate ranking and threshold logic |

### 15.2 Integration tests

Fixture repos or synthetic temp repos simulating:

- Shared library dependency
- HTTP provider/consumer pair
- Event producer/consumer pair
- gRPC provider/consumer pair
- Unrelated repo with similar symbol names
- Missing GitHub App token or failed remote discovery
- Zero-match scenario

Verify:

- Local-only review output is identical when multi-repo is unavailable
- Only the correct repos are selected
- Unrelated repos are excluded
- Remote snippet fetch happens only after candidate selection

### 15.3 Acceptance criteria

The multi-repo feature is complete when:

1. Every PR run builds graphs fresh without persistent storage
2. Repo discovery requires no manual registry or developer config
3. Candidate selection is deterministic and explainable
4. Agents can fetch exact remote snippets only for selected repos
5. False positives from random symbol matches are reduced by namespace/rule checks
6. Current single-repo reviews work without functional regression
7. **Developers do not need to change any workflow file, annotation, or config to benefit from multi-repo review**

---

## 16. Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| Building all repo graphs is slow | Shallow checkout, lightweight remote graph mode, bounded worker concurrency, skip archived/fork/empty repos |
| Too many repos in the installation | Constrain to installation scope, apply deterministic ranking before LLM context load, cap at 10 candidates |
| Dynamic routing or reflection hides dependencies | Support only dominant patterns in v1, allow low-confidence comments with explicit uncertainty |
| False positives from weak symbol matches | Require namespace/module confirmation, require fetched snippet evidence for strong findings |
| GitHub App rate limiting | Add retry with backoff, degrade gracefully to single-repo review |
| Large temp disk usage from clones | Shallow checkout (`--depth 1`), cleanup on job exit, skip binary/vendor dirs |

---

## 17. What Not To Do

- ❌ Commit graph snapshots into repos
- ❌ Add artifact/index storage in the initial phases
- ❌ Add Actions cache persistence for remote graphs
- ❌ Introduce a repo registry or service map
- ❌ Depend on hand-maintained API contracts
- ❌ Replace the current single-repo flow
- ❌ Let the LLM guess candidate repos by itself
- ❌ Send full remote repos to Gemini
- ❌ Require developers to add any config, flag, or annotation
- ❌ Add mandatory new inputs to `action.yml`

---

## 18. Future Scope (Post v1)

These are explicitly deferred and not part of this plan:

- Graph caching via GitHub Actions cache
- Nightly indexing workflows for pre-built graphs
- Multi-organization support (multiple installations)
- Cross-repo impact scoring and trend tracking
- Dashboard integration for cross-repo insights
- Support for monorepo sub-project discovery
- Configurable repo allowlists/denylists (for enterprise teams)

---

## 19. Summary

| Aspect | Decision |
|--------|----------|
| Developer effort | **Zero** — automatic detection |
| Activation trigger | GitHub App with 2+ repo access |
| Fallback | Silent degradation to single-repo review |
| Graph storage | None — built fresh each run |
| Repo discovery | Automatic from App installation |
| Candidate selection | Deterministic code-based matching |
| LLM behavior | Uses match summary + on-demand snippets |
| Existing review | **Completely unchanged** |
