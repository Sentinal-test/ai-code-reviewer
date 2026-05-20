# Multi-Repo Dependency Discovery — Implementation Plan

> **Goal:** When a PR/MR is opened in any repo, automatically detect every other repo in the org that consumes the changed code, fetch just enough of those repos, and let the review agents flag downstream breakage. **Zero developer configuration.**

This plan focuses on the *discovery* problem — "which peer repos are affected?" — because cloning, graphing, matching, and agent tooling are already implemented. Discovery is the bottleneck that limits both coverage (we miss real consumers) and cost (we clone repos that don't matter).

---

## 1. What we have today

Walking `cmd/cli/main.go:266-349` and the supporting packages:

| Stage | File | What it does | Limitation |
|---|---|---|---|
| Gate | `cmd/cli/main.go:274` | `scmProvider == "github" && isGitHub && checkMultiRepoAvailability(...)` | GitLab is silently dropped to single-repo mode |
| List peers | `internal/platform/github/github.go:344` (`ListAccessibleRepos`) | `Apps.ListRepos` — every repo the App is installed on | One paged API call, no filtering by topic/language/team |
| Filter peers | `internal/multirepo/discovery.go:22` (`DiscoverRepos`) | Drops current/archived/disabled/empty/forks; restricts to same owner | Doesn't yet exclude obviously irrelevant repos (docs-only, infra-only) |
| Relevance prefilter | `internal/reposelect/match.go:121` (`IdentifyRelevantRepos`) | Fetches `go.mod`, `package.json`, etc. (7 hardcoded filenames) **at repo root only**, string-matches local module name | **Misses monorepos with nested manifests; one HTTP call per repo per manifest filename (O(repos × 7))** |
| Clone | `internal/multirepo/checkout.go` | `git clone --depth 1 --branch <default>` into `os.MkdirTemp`, **wiped at process exit** | No persistence — every PR repays full clone+parse cost |
| Build graph | `internal/codegraph/remote_graph.go` | Tree-sitter walks every source file → `RemoteRepoGraph` with `Symbols` and `InvertedReferences` | Already serializable (JSON tags + `CommitSHA`), but never written to disk |
| Match | `internal/reposelect/match.go:368` (`MatchRepos`) | Cross-references local `ChangedExports` against remote `Imports` and `Symbols` | Solid, no changes needed |
| Agent tools | `internal/llm/tools.go:104-194` | `list_cross_repo_matches`, `resolve_repo_symbol`, `get_repo_callers`, `fetch_repo_snippet`, `search_repo_graph` | Solid, no changes needed |

**Verdict:** The pipeline downstream of "which repos matter?" is mature. The discovery and fetching layer is the weak link.

---

## 2. The discovery problem, framed precisely

Given:
- A PR in `org/svc-A` that touches files `X` and `Y`
- A set of exported symbols `S = {f, g, h}` whose signature or existence changed
- An import path `P` (e.g., `github.com/org/svc-A/pkg/auth`)

We need to produce: **the minimal set of repos `R ⊆ org` where one of these is true:**

1. The repo's manifest declares a dependency on `P` (Go module, npm package, Maven coordinate, etc.)
2. The repo's source code imports `P` (or a sub-path of it)
3. The repo references symbols in `S` via reflection / dynamic lookup / config files (rare, but real for plugin systems)

Conditions 1 + 2 cover ~99% of real cases. Condition 3 is out of scope for now.

The naive answer ("clone everything, grep") works but is O(org × repo size) — for a 100-repo org that's GB of transfer per PR. We need a discovery path that's **O(API calls)** before we touch git at all.

---

## 3. Discovery strategies, compared

We have five practical sources of "does repo X depend on module P?" data. Each is rated on:

- **Coverage**: false negative rate (does it miss real consumers?)
- **Cost**: API calls and latency per PR
- **Freshness**: how stale is the answer?
- **Auth**: what permissions are required?
- **Setup**: developer/admin work to enable

### Strategy A — Current: per-repo manifest fetch via Contents API

> What `IdentifyRelevantRepos` does today.

| | Score |
|---|---|
| Coverage | **Low** — root-only, only 7 manifest filenames, misses monorepos and language ecosystems we didn't enumerate |
| Cost | High — O(repos × manifests) HTTP calls; ~50 repos × 7 manifests = 350 calls per PR |
| Freshness | Real-time |
| Auth | `contents:read` per repo |
| Setup | Already done |

**Keep as fallback only.** Replace as primary.

### Strategy B — GitHub Dependency Graph / SBOM API

> Dependabot already parses every manifest in every repo on every push. We can query the result.

Endpoints:
- `GET /repos/{owner}/{repo}/dependency-graph/sbom` — full SPDX SBOM, every manifest in the repo tree, all transitive deps
- GraphQL: `repository.dependencyGraphManifests.dependencies` — same data, queryable

| | Score |
|---|---|
| Coverage | **High** — covers all manifest types GitHub supports (Go, npm, pip, Maven, Gradle, Cargo, NuGet, RubyGems, Composer, Pub, Swift, Hex, Actions), handles nested manifests in monorepos |
| Cost | **Low** — 1 API call per repo (can be batched over GraphQL). For 50-repo org: 50 calls, or ~5 GraphQL batches |
| Freshness | Updated on every push to default branch by Dependabot; lag ~minutes |
| Auth | `metadata:read` is enough on public repos; private repos require `contents:read` |
| Setup | **Zero** — Dependency Graph is on by default for public repos; enabled per-org for private (one toggle in org settings) |

**Recommended primary for GitHub.** This is the closest thing to a "what depends on what?" index that already exists in the org.

### Strategy C — GitHub Code Search API

> Search `org:foo "github.com/org/svc-A/pkg/auth"` across the entire org in one call.

| | Score |
|---|---|
| Coverage | **High for import-statement search** — catches usages even when manifest is missing (vendored, replace directives, internal copies) |
| Cost | **Very low** — 1 API call per changed import path. Returns `path` + line numbers, so we already know where the consumer code lives |
| Freshness | Real-time (indexed continuously) |
| Auth | `metadata:read` works for public repos; private repos need the App installed on each repo (which it already is) |
| Setup | Zero — code search is GA |

Limit: 30 req/min for code search, 1000 results per query (paginated 100 at a time). For PRs that change ≤30 import paths, this is one round-trip.

**Recommended primary for finding actual usage sites** (complements B which finds manifest declarations).

### Strategy D — Org-wide pre-indexed graph cache

> Build the `RemoteRepoGraph` once per (repo, commit) and persist it. Discovery becomes a lookup against the cached graphs.

| | Score |
|---|---|
| Coverage | **Highest** — works on actual parsed code, catches symbol-level usage not just import-level |
| Cost | **Lowest at PR time** — 0 clones, 0 API calls; just read N JSON blobs from S3/cache |
| Freshness | Depends on indexing trigger — if on every default-branch push, lag is one CI run |
| Auth | Read access to the cache bucket |
| Setup | **One-time:** ship an indexing GitHub Action / GitLab CI job to every repo in the org. Reusable workflow makes this a 2-line addition per repo (or org-wide via repository rulesets / GitLab compliance pipelines) |

**Recommended endgame.** Layer this on top of B + C — it doesn't replace them, it caches their results.

### Strategy E — GitLab Dependency List API + Group Code Search

GitLab's equivalent of B and C, accessed via the group:

- `GET /groups/:id/dependencies` (or `/projects/:id/dependencies`) — Dependency Scanning API
- `GET /groups/:id/search?scope=blobs&search=...` — search across all projects in the group in one call
- `GET /groups/:id/projects?include_subgroups=true` — list all projects under a group/subgroup tree

| | Score |
|---|---|
| Coverage | **High** for code search; Dependency Scanning requires the GitLab Dependency Scanner job which is part of Ultimate or self-enabled CI |
| Cost | Low — 1 group-level call per discovery |
| Freshness | Real-time for search; depends on last scan run for Dependency Scanning |
| Auth | Group access token / service-account PAT with `read_api + read_repository` on the parent group (you already have this — see your memory note on group CI/CD variables) |
| Setup | Zero for code search; for Dependency Scanning, enable in a group-level `.gitlab-ci.yml` template |

**Recommended primary for GitLab.**

---

## 4. Recommended design

Layer the sources so we always get a fast answer and degrade gracefully.

```
┌─────────────────────────────────────────────────────────────────┐
│ Layer 0: Org-level Graph Cache (D)                              │
│  - Read pre-built RemoteRepoGraph JSON from S3 / GHA cache      │
│  - Keyed by {owner}/{repo}/{default_branch_sha}.json.zst        │
│  - Hit → skip everything below, go straight to matching         │
└─────────────────────────────────────────────────────────────────┘
                              ↓ miss
┌─────────────────────────────────────────────────────────────────┐
│ Layer 1: Manifest Index (B for GitHub, E for GitLab)            │
│  - GitHub: GraphQL batch query dependencyGraphManifests         │
│  - GitLab: GET /groups/:id/dependencies                         │
│  - Returns: list of repos declaring our changed module as dep   │
└─────────────────────────────────────────────────────────────────┘
                              ↓ union with
┌─────────────────────────────────────────────────────────────────┐
│ Layer 2: Source Code Search (C for GitHub, E for GitLab)        │
│  - GitHub: search/code?q=org:X "<importPath>"                   │
│  - GitLab: GET /groups/:id/search?scope=blobs&search=...        │
│  - Returns: list of repos with import statements referencing P  │
│  - Catches monorepos, vendored copies, replace directives       │
└─────────────────────────────────────────────────────────────────┘
                              ↓ union
                  candidateRepos := L0 ∪ L1 ∪ L2
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│ Layer 3: Existing Pipeline (unchanged)                          │
│  - Shallow clone only `candidateRepos`                          │
│  - BuildRemoteGraph → MatchRepos → agent tools                  │
└─────────────────────────────────────────────────────────────────┘
                              ↓ background
┌─────────────────────────────────────────────────────────────────┐
│ Side effect: write fresh graph to Layer 0 cache (warms cache)   │
└─────────────────────────────────────────────────────────────────┘
```

**Why this order:**
- Layer 0 is "free" on cache hit — most PRs in a stable org hit cache for 90%+ of peers.
- Layer 1 (manifest data) is the most authoritative for "declares a dep." Catches the easy 80%.
- Layer 2 (code search) catches the long tail (monorepos, internal mirrors, vendored code).
- Existing pipeline does what it does today, just with a much smaller `candidateRepos` set.
- Cache warming on success means **the first review of repo-X is slow, every subsequent review is fast**.

---

## 5. The "best" answer per platform

### GitHub — recommended primary: **Layer 1 (Dependency Graph) + Layer 2 (Code Search)**, with Layer 0 added in phase 2

**Why both:** Dependency Graph misses code-only usages (vendoring, replace, internal copies). Code Search misses transitive dependencies. Together they have negligible false negatives.

**One required org setting:** enable "Dependency graph for private repositories" in Org Settings → Code security and analysis. Zero per-repo config. Zero developer config.

**Concrete API plan:**

```graphql
# Layer 1 — one query, up to 100 repos per page
query($org: String!, $cursor: String) {
  organization(login: $org) {
    repositories(first: 100, after: $cursor) {
      pageInfo { hasNextPage endCursor }
      nodes {
        nameWithOwner
        dependencyGraphManifests(first: 10) {
          nodes {
            blobPath
            dependencies(first: 100) {
              nodes { packageName packageManager requirements }
            }
          }
        }
      }
    }
  }
}
```

Run once, build an in-memory inverted index: `packageName → [repos that depend on it]`. Cache this index for 1h (Dependabot updates are infrequent). For a 100-repo org, this is a single GraphQL call returning ~2 MB.

```http
# Layer 2 — one HTTP call per changed import path
GET /search/code?q=org:myorg+%22github.com%2Fmyorg%2Fsvc-a%2Fpkg%2Fauth%22&per_page=100
```

### GitLab — recommended primary: **Layer 2 (Group Search)**, with Layer 1 added if Dependency Scanning is enabled

GitLab's Dependency Scanning is only useful if the org already runs it. Group Blob Search works for everyone with `read_api`.

**Auth:** Your existing **service-account PAT in the group CI/CD variable** (per memory note) is exactly the right credential. Need scopes `read_api + read_repository` on the parent group.

```http
# List all projects under the group (one-time discovery, cacheable)
GET /api/v4/groups/{group_id}/projects?include_subgroups=true&per_page=100

# Layer 2 — one call per changed import path
GET /api/v4/groups/{group_id}/search?scope=blobs&search=svc-a/pkg/auth
```

GitLab's blob search returns `project_id`, `path`, `startline` — same shape as GitHub Code Search, integrates cleanly.

### Both platforms — phase 2: Layer 0 (org-level graph cache)

Indexing trigger:
- **GitHub:** an org-level reusable workflow that runs on every `push` to default branch, builds the graph, uploads to S3 (or GitHub Actions cache with a global key). Roll out via [Repository Rulesets](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets) or organization-required workflows.
- **GitLab:** a [compliance pipeline](https://docs.gitlab.com/ee/user/group/compliance_pipelines.html) at the group level that runs the same indexing job on default-branch pushes.

Storage:
- S3 bucket `s3://code-review-graphs/{owner}/{repo}/{sha}.json.zst` — `RemoteRepoGraph` already has `json` tags and a `CommitSHA` field
- Optional: a small Redis/Postgres index `{owner}/{repo} → latest_sha` to skip the GitHub call

---

## 6. Implementation phases

### Phase 1 — Replace prefilter with org-level discovery (high value, ~1 week)

**Files to touch:**
- New: `internal/reposelect/discovery_github.go` — GraphQL client for `dependencyGraphManifests` + code search; builds an inverted index `packageName → []repos`
- New: `internal/reposelect/discovery_gitlab.go` — `/groups/:id/projects` + `/groups/:id/search?scope=blobs`
- New: `internal/reposelect/discovery.go` — interface `Discoverer` with `FindCandidates(ctx, signals LocalSignals) []DiscoveredRepo`
- Replace: `IdentifyRelevantRepos` call in `cmd/cli/main.go:292` with `discoverer.FindCandidates(...)`
- Keep: the existing manifest-fetch path as fallback when API quota is exhausted

**Acceptance:** for a test org with 20 repos, `FindCandidates` returns the correct set in < 2s using ≤ 5 API calls.

### Phase 2 — GitLab parity (~3 days)

**Files to touch:**
- New: `internal/platform/gitlab/discover.go` — `ListGroupProjects(groupID)`
- New: `gitlab.GitLabClient.GroupID()` accessor
- Modify: `cmd/cli/main.go:274` gate from `isGitHub && checkMultiRepoAvailability(...)` to `discoverer != nil` (provider-agnostic)
- Reuse: `multirepo.WorkerPool`, `multirepo.ShallowCheckout` (just pass `gitlab-ci-token:<pat>` in URL — `checkout.go:16` already handles arbitrary token strings)

**Acceptance:** a GitLab MR in a group with sibling projects produces cross-repo matches identical in shape to GitHub.

### Phase 3 — Org-level pre-indexed graph cache (~1-2 weeks)

**Files to touch:**
- New: `cmd/cli/main.go` subcommand `code-review index` that calls `BuildRemoteGraph` and uploads to S3
- New: `internal/codegraph/cache.go` — `LoadCachedGraph(owner, repo, sha)` + `SaveCachedGraph(...)` (S3, GHA cache, or local disk based on config)
- Modify: `BuildRemoteGraph` callsite at `main.go:314` to check cache first
- New: `.github/workflows/index-graph.yml` reusable workflow + GitLab `index-graph.yml` template
- Rollout: org repository ruleset / GitLab compliance pipeline to require the indexing workflow

**Acceptance:** on a 50-repo org after one warmup week, ≥ 90% of peer graphs are served from cache; cold-start indexing latency drops from minutes to ~5s per PR.

### Phase 4 — API-surface diffing (the "actually flag breakage" piece)

**Files to touch:**
- New: `internal/codegraph/api_diff.go` — given the local `Graph` before and after the diff, produce `APIChanges{Removed, Renamed, SignatureChanged}` for exported symbols
- Modify: orchestrator at `internal/orchestrator/orchestrator.go` — for each `APIChange.Removed`, pre-resolve callers across `remoteGraphs` and inject as Differentiated Context for the Structure agent (no tool call needed)
- New tool: `find_breaking_consumers(symbol, old_signature, new_signature)` in `internal/llm/tools.go` — deterministic arity/type mismatch detection

This is the difference between "agent might notice" and "agent will flag, with exact file:line evidence."

---

## 7. What each layer does for the developer

**Required developer action: none.** Detection runs automatically based on what's in the PR + what the discovery layer finds.

Required org-admin action (one-time):
- **GitHub:** flip "Dependency graph for private repositories" to on. (Optional, phase 3: install indexing reusable workflow as a required workflow.)
- **GitLab:** ensure the service-account PAT in the group CI/CD variable has `read_api + read_repository` at group scope. (Optional, phase 3: add indexing template to compliance pipeline.)

---

## 8. Sequencing recommendation

Start in this order — each phase is independently shippable and delivers value before the next:

1. **Phase 1 (Discovery)** — replaces the weakest part of the current system with authoritative data. Biggest accuracy win.
2. **Phase 2 (GitLab parity)** — unblocks the entire feature for GitLab orgs. Small effort, large unlock.
3. **Phase 4 (API-surface diffing)** — improves *review quality* directly. Doesn't depend on phases 1-3 but compounds with them.
4. **Phase 3 (Graph cache)** — performance + cost. Worth doing last because the org-wide rollout has more friction than the others.

Total effort to reach "automatic, accurate, cross-platform multi-repo reviews": roughly 3-4 weeks of focused work, broken into shippable slices.

---

## 9. Open questions to resolve before implementation

1. **Cache storage:** S3 vs. GitHub Actions cache vs. self-hosted MinIO? S3 is simplest if you already have AWS; GHA cache is free but per-repo (limits cross-repo reuse).
2. **GitHub API rate limits:** Code Search is 30 req/min per user — fine for one PR, but a large monorepo PR with 50 changed imports needs batching. Solution: dedupe to unique import paths first; most PRs hit 1-3 paths.
3. **GitLab Dependency Scanning availability:** confirm whether the org runs it on Ultimate; if not, Layer 1 for GitLab is skipped entirely and we rely on Layer 2.
4. **Symbol-level vs. import-level matching:** current code-search only matches import paths. A removed exported function in a still-imported package won't be flagged at discovery time — only when the agent later runs `get_repo_callers`. Phase 4 closes this.
5. **Private vendored copies:** if a peer repo vendored your code (e.g., `vendor/github.com/org/svc-a/...`), code search will match but the consumer doesn't depend on *your* repo via a package manager. Treat as a real consumer for safety — false positives cost a clone, false negatives miss bugs.

---

## 10. TL;DR

| Question | Answer |
|---|---|
| What's wrong today? | Discovery uses 7 root-only manifest files via per-repo API calls; misses monorepos; GitLab has no multi-repo at all |
| What to fetch? | (a) Dependabot's Dependency Graph SBOM per repo, (b) code search results for changed import paths, (c) cached pre-built `RemoteRepoGraph` if available |
| How to find dependent repos? | GitHub: GraphQL `dependencyGraphManifests` + `search/code`. GitLab: `/groups/:id/projects` + `/groups/:id/search?scope=blobs`. Both auto, both org-level, both zero developer config |
| Best single thing to build first? | Replace `IdentifyRelevantRepos` with the layered discoverer (Phase 1) — biggest accuracy + cost win for least effort |
| Endgame? | Org-level graph cache: every default-branch push indexes itself, PR reviews read graphs from cache instead of cloning |
