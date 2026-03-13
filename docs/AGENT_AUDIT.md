# Agent Audit

## Scope

This audit is based on a review of the implemented code paths in `backend/cmd/cli/main.go:44`, `backend/internal/orchestrator/orchestrator.go:57`, `backend/internal/llm/agent_loop.go:61`, `backend/internal/llm/tools.go:15`, `backend/internal/action/github.go:204`, `backend/main.go:33`, `action.yml:1`, and `.github/workflows/ai-review.yml:1`, plus the supporting code graph, rules, auth, and documentation modules.

## Executive Verdict

The agent is already stronger than a basic single-prompt PR reviewer. The GitHub Action / CLI path is well designed: it gathers diffs locally, builds deterministic code context, splits large PRs into chunks, runs three specialist agents in parallel, and consolidates their findings before posting review comments.

That said, the current implementation is best described as a strong changed-code reviewer, not a complete PR assurance system.

It is good at:

- reviewing changed code with code-aware context
- checking related definitions and callers with tools
- handling large PRs better than a single-pass prompt
- adding some PR intent via title, body, and commit messages
- doing limited cross-repo reasoning when multi-repo mode is active

It is not yet good enough to claim:

- guaranteed line-by-line review coverage of every PR line
- reliable validation against user stories or acceptance criteria
- deterministic checking of config, contracts, migrations, docs, or operational impact
- consistent behavior across all runtime modes

## What Is Implemented Today

### 1. Action-mode review flow is the real production path

The primary pipeline is:

- workflow trigger in `.github/workflows/ai-review.yml:1`
- composite action wrapper in `action.yml:1`
- CLI entrypoint in `backend/cmd/cli/main.go:44`
- code graph context build in `backend/internal/codegraph/service.go:53`
- chunking in `backend/internal/chunker/chunker.go:34`
- three-agent fanout in `backend/internal/orchestrator/orchestrator.go:57`
- tool-driven agent loop in `backend/internal/llm/agent_loop.go:61`
- GitHub posting with fallback in `backend/internal/action/github.go:204`

This is a solid architecture.

### 2. The agent already gets more than just raw diff text

The current implementation gives the model:

- full changed-file content with line numbers in `backend/internal/llm/agent_loop.go:262`
- inline diff hunks with explicit old/new markers in `backend/internal/llm/agent_loop.go:293`
- PR title, body, and commit messages in `backend/internal/llm/agent_loop.go:350`
- deterministic dependency summaries from the code graph in `backend/internal/codegraph/service.go:53`
- repository structure for the structure agent in `backend/internal/orchestrator/orchestrator.go:141`
- tool access for definitions, callers, file reads, code search, and multi-repo lookups in `backend/internal/llm/tools.go:15`
- developer rules, focus areas, and ignore patterns in `backend/internal/rules/rules.go:133`

So the system is already context-aware. It is not a blind diff reviewer.

### 3. Multi-agent specialization is a real strength

The orchestrator runs correctness, security, and structure agents in parallel in `backend/internal/orchestrator/orchestrator.go:151`. This is materially better than one generic prompt because:

- each agent has a focused prompt in `backend/internal/agents/prompts.go:82`, `backend/internal/agents/prompts.go:237`, and `backend/internal/agents/prompts.go:360`
- correctness and security get slim dependency context in `backend/internal/orchestrator/orchestrator.go:133`
- structure gets the repo tree in `backend/internal/orchestrator/orchestrator.go:141`
- agents can verify assumptions via tools before reporting defects in `backend/internal/agents/prompts.go:53`

This part of the design is good and worth keeping.

## Direct Answers To Your Questions

### How is the agent overall?

Overall, the agent is good and directionally correct.

It has a real review architecture, not just prompt engineering. The strongest parts are deterministic preprocessing, chunking, specialist agents, tool use, and multi-repo extension points.

My assessment:

- architecture quality: strong
- changed-code review quality: good
- project-context awareness: partial
- business/user-story validation: weak today
- end-to-end PR assurance: incomplete today
- operational maturity: mixed because Action mode is much stronger than SaaS mode

### Is there anything wrong?

Yes. The biggest issues are architectural and product-scope issues, not just bugs.

#### A. The system skips docs and config by design

The orchestrator explicitly filters out documentation and config files in `backend/internal/orchestrator/orchestrator.go:17` and `backend/internal/orchestrator/orchestrator.go:79`.

That means the system will not properly review many high-impact PR changes such as:

- `package.json`
- `go.mod`
- lockfiles
- workflow files
- deployment config
- environment changes
- docs that describe intended behavior

This is one of the clearest gaps between "reviews changed code" and "reviews the whole PR."

#### B. "Review every line of the PR" is not guaranteed

The system shows changed files to the model, but it does not maintain a deterministic audit trail proving every hunk or line was actually checked.

Limitations include:

- review scope is restricted to `+` lines by prompt rules in `backend/internal/agents/prompts.go:10`
- comment posting snaps to the nearest valid diff line in `backend/internal/action/github.go:87`
- oversized files fall back to focused diff windows rather than full-file injection in `backend/internal/llm/agent_loop.go:276`
- docs/config are filtered out before specialist review in `backend/internal/orchestrator/orchestrator.go:79`

So the answer is: it likely sees most changed code lines in Action mode, but it does not guarantee true line-by-line review coverage.

#### C. There are two different architectures in the repo

The Action / CLI path uses the newer multi-agent orchestrator in `backend/cmd/cli/main.go:365`.

The server / webhook path still uses the older single-pass `RunReview` flow in `backend/main.go:328`.

This means:

- capabilities differ by runtime mode
- results can differ for the same PR
- maintenance is duplicated
- docs can drift because both paths exist

This is a major product consistency issue.

#### D. User-story validation is mostly missing

The model gets PR metadata and free-form developer rules, but there is no first-class structure for:

- user stories
- acceptance criteria
- linked issue requirements
- domain rules
- release constraints
- test expectations

Today, the closest thing is PR title/body/commits in `backend/internal/llm/agent_loop.go:350` and `review_rules` in `backend/internal/rules/rules.go:133`.

That is useful, but it is not enough for reliable business validation.

#### E. Non-code correctness is largely uncovered

There is no deterministic reviewer for:

- API contracts
- migrations
- schema changes
- env/config compatibility
- operational runbooks
- docs vs implementation consistency

This means a PR can be "code-correct" and still be product-wrong.

### Is there anything we miss?

Yes. The main missing capabilities are:

1. structured requirements context
2. deterministic hunk coverage tracking
3. first-class contract and migration review
4. CI/test-result context
5. review of docs/config/ops changes
6. project memory across PR revisions and prior comments
7. richer graph metadata such as routes, events, schemas, env vars, and public APIs
8. unified behavior across Action mode and server mode

### What further can we do?

The highest-value next steps are:

1. add structured business context
2. add deterministic coverage accounting
3. add deterministic reviewers for non-code artifacts
4. unify runtime paths
5. improve tooling and retrieval

These are detailed in the roadmap section below.

### Is the agent able to review each and every line of a PR?

Not fully, and I would avoid claiming that today.

Accurate answer:

- for changed code in Action mode, it probably inspects almost all relevant changed code lines
- for very large files, it may inspect windows around changes rather than the whole file
- for docs/config/non-code files, it intentionally skips review
- for GitHub inline comments, the final posted line may be snapped to the nearest valid diff line
- there is no explicit "all hunks processed" verifier

So the safer product statement is:

"The agent performs chunked, multi-agent review across changed code with supporting repository context, but it does not yet provide deterministic proof that every PR line and artifact was reviewed."

### Can we add more context to the LLM regarding project context, user stories, and PR intent rather than only code?

Yes, and you should.

In fact, this is probably the single most important product improvement after coverage tracking.

The current design already supports adding extra context blocks before analysis in `backend/internal/llm/agent_loop.go:253`, so the product architecture can accommodate this.

You should add structured inputs for:

- project overview
- domain glossary
- business rules
- user story or ticket description
- acceptance criteria
- affected personas or workflows
- rollout constraints
- backward compatibility expectations
- linked design docs / ADRs
- known areas that must not regress

### Can the LLM judge whether the implemented logic is correct according to the user story?

Only partially today.

Right now it can do this only when the user story is indirectly present in:

- PR title/body
- commit messages
- developer review rules
- the code itself

That means it can sometimes infer intent, but it cannot reliably validate correctness against a formal user story because that artifact is not ingested as structured context.

So the correct answer is:

- today: partial and heuristic
- after structured requirements ingestion: much better
- after adding deterministic acceptance checks and contract reviewers: meaningfully reliable

### Are there more things like this beyond user stories?

Yes. User stories are just one category of missing non-code context.

Other missing context types include:

- architecture decisions
- service boundaries
- API contracts
- event schemas
- migration requirements
- tenant isolation rules
- security model assumptions
- rollout / feature-flag expectations
- operational constraints
- test plans and expected failure modes
- ownership and domain boundaries

## What Context The LLM Has Today

Today the LLM has access to four kinds of context.

### 1. Changed code context

- full changed-file content for normal-size files in `backend/internal/llm/agent_loop.go:271`
- diff hunks with old/new direction markers in `backend/internal/llm/agent_loop.go:294`

### 2. Repository context

- dependency summaries from the code graph in `backend/internal/codegraph/service.go:53`
- repository structure for the structure agent in `backend/internal/orchestrator/orchestrator.go:141`

### 3. Intent context

- PR title/body/commits in `backend/internal/llm/agent_loop.go:350`
- developer instructions/focus in `backend/internal/rules/rules.go:146`

### 4. Retrieval / verification context

- symbol lookup in `backend/internal/llm/tools.go:19`
- file retrieval in `backend/internal/llm/tools.go:33`
- caller lookup in `backend/internal/llm/tools.go:47`
- search in `backend/internal/llm/tools.go:61`
- cross-repo graph tools in `backend/internal/llm/tools.go:75`

This is good foundation context, but it is still mostly code-centered context.

## What Context Is Missing Today

The missing context can be grouped into six buckets.

### 1. Product context

Missing:

- business goals
- user stories
- acceptance criteria
- workflows and user journeys
- personas and role expectations

Impact:

The LLM can say whether code looks plausible, but not whether it solves the correct problem.

### 2. Contract context

Missing:

- OpenAPI / GraphQL / proto / JSON schema diffs
- DB migration intent
- event/message contracts
- compatibility rules

Impact:

Breaking changes can slip through even if the changed code itself looks reasonable.

### 3. Operational context

Missing:

- environment variable contracts
- deployment assumptions
- infra/workflow implications
- feature flags and rollout strategy
- CI results and failing tests

Impact:

The reviewer may miss production-impacting changes outside the narrow code path.

### 4. Historical context

Missing:

- prior review comments
- previous PR revisions
- known recurring defects
- bug history for the touched area

Impact:

The reviewer treats each run as mostly stateless.

### 5. Broader repository navigation

Missing:

- a file-list tool
- stronger semantic search
- richer path discovery for the correctness/security agents

Impact:

The model can inspect files it knows about, but discovery is still weaker than it could be.

### 6. Domain semantics in the graph

The remote graph explicitly leaves placeholders for richer extraction in `backend/internal/codegraph/remote_graph.go:27`.

Missing graph facts include:

- HTTP routes
- event topics
- gRPC services
- env vars
- schema ownership
- public API surfaces

Impact:

Cross-service reasoning remains heuristic instead of deterministic.

## Main Risks In The Current Implementation

### 1. Product claims may outrun actual behavior

If you say "reviews every line of every PR," that is stronger than the implementation.

### 2. Important artifacts are skipped

Skipping docs/config may reduce noise, but it also removes review coverage from many production-critical changes.

### 3. Behavior differs by deployment mode

The newer Action path is significantly stronger than the older webhook path.

### 4. Context is still too code-heavy

The model knows a lot about the code and much less about the product requirements behind the code.

### 5. Coverage is probabilistic, not auditable

There is no hunk-by-hunk manifest showing what was reviewed, skipped, or escalated.

## Concrete Implementation Issues Worth Fixing

These are smaller than the product gaps above, but they are still real issues.

### 1. Unused action input

`allowed_domain` is declared in `action.yml:30`, but it is not used by the Action flow.

### 2. Response schema is declared but not actually enforced

`backend/internal/llm/agent_loop.go:19` declares `responseSchema` and comments that Gemini output is schema-enforced, but the current request flow only toggles `ResponseJSON` in `backend/internal/llm/agent_loop.go:133`.

That means the implementation is weaker than the comment suggests.

### 3. Provider validation in the action is too loose

The action resolves all three provider keys in `action.yml:99`, but the validation in `action.yml:109` only checks whether any provider key exists.

That can produce confusing behavior when the selected provider and the available key do not match.

### 4. PR context is still shallow

`backend/internal/action/github.go:405` fetches commit messages, but only keeps the last two commits in `backend/internal/action/github.go:411`.

That is reasonable for token control, but it also means the intent context is intentionally shallow.

### 5. Search tooling is capped

`search_codebase` uses `git grep --max-count=5` in `backend/internal/llm/tools.go:309` and then caps output again in `backend/internal/llm/tools.go:321`.

This helps control noise, but it can hide broader impact when a changed symbol is used widely.

## Recommended Roadmap

## Priority 1: Add deterministic review coverage

Build a review manifest that tracks every changed file and hunk.

Minimum behavior:

- enumerate all changed hunks before review
- mark each hunk as assigned to a chunk
- require a post-pass verifier to confirm no hunk was skipped
- report explicit coverage stats in the final summary
- distinguish code hunks, config hunks, docs hunks, and unsupported hunks

This is the most important step if you want to claim broad PR coverage.

## Priority 2: Add structured requirements context

Introduce first-class project and PR context that is not buried in free-form prompt text.

Recommended context model:

- project overview
- domain glossary
- business rules
- linked ticket / user story
- acceptance criteria
- out-of-scope notes
- rollback constraints
- test plan
- compatibility expectations

Best places to source it from:

- PR template fields
- linked Jira / Linear / GitHub issues
- repository-level context files
- service-level context files
- manually supplied review metadata in CI

## Priority 3: Add deterministic non-code reviewers

Add specialized review passes for:

- API contract changes
- schema/migration changes
- config/env changes
- workflow / deployment changes
- docs vs implementation consistency

These should not rely only on one LLM pass. They should use deterministic checks where possible.

## Priority 4: Unify the runtime architecture

Move the server path onto the same multi-agent orchestrator used by the Action path.

Relevant split today:

- new path: `backend/cmd/cli/main.go:365`
- old path: `backend/main.go:328`

Until this is unified, product behavior will remain inconsistent.

## Priority 5: Make business validation first-class

Add a dedicated "requirements / intent" reviewer whose job is:

- read structured story and acceptance criteria
- compare implementation against stated intent
- identify missing branches, edge cases, or misinterpreted requirements
- call out when PR intent and code behavior diverge

This is distinct from correctness and should not be hidden inside the correctness prompt.

## Priority 6: Enrich the code graph with domain facts

Extend graph extraction to include:

- routes and handlers
- request/response schemas
- event producers and consumers
- database models and migrations
- env var usage
- public API surfaces

This will improve both correctness and cross-repo review quality.

## Priority 7: Improve retrieval and evidence gathering

Add tools for:

- listing files
- finding similar implementations
- retrieving specs / ADRs / docs
- querying prior review comments
- reading CI/test output

The goal is to let the model verify assumptions instead of inferring them.

## Priority 8: Review docs and config instead of blanket skipping them

Do not send all docs/config through the current code specialist prompts, but do create separate reviewers for:

- config risk
- CI/workflow changes
- dependency updates
- docs/implementation drift

## Proposed Future Context Model

If you want the LLM to judge whether a PR is correct beyond code, the review input should eventually include these categories:

- repository context: what this project does, major modules, architecture constraints
- domain context: business vocabulary, invariants, security model, tenant rules
- change intent: what this PR is supposed to accomplish
- user story: the problem, actor, expected outcome
- acceptance criteria: concrete conditions that must be true
- contract context: APIs, schemas, events, migrations, compatibility rules
- operational context: env vars, rollout plan, feature flags, SLO-sensitive paths
- validation context: tests run, failures, snapshots, screenshots, benchmark deltas

Once you have this, the LLM can answer better questions such as:

- does this implement the intended workflow?
- does the logic satisfy the acceptance criteria?
- did this PR break a downstream contract?
- are important rollout or migration steps missing?

## Suggested Product Positioning Today

What you can confidently say now:

- the agent performs multi-agent review of changed code with deterministic repository context and tool-assisted verification
- it uses PR metadata and developer rules to improve intent awareness
- it can do limited cross-repo analysis when multi-repo context is available

What you should not claim yet:

- complete review of every PR line and artifact
- reliable validation against user stories or acceptance criteria
- full project understanding beyond code and PR text

## Additional Observations

### Documentation drift

Some docs still describe older flows or over-simplify the current behavior. For example, `docs/LLM_CONTEXT_STRATEGY.md:5` describes a two-pass scout/review system, while the implemented Action path is now a chunked multi-agent architecture centered around `backend/cmd/cli/main.go:365` and `backend/internal/orchestrator/orchestrator.go:57`.

### SaaS/dashboard path looks earlier-stage than the Action path

The auth layer still contains localhost and company-specific assumptions in `backend/internal/auth/auth.go:29`, `backend/internal/auth/auth.go:117`, `backend/internal/auth/auth.go:146`, and `backend/internal/auth/auth.go:152`.

That does not directly weaken Action-mode PR review, but it does show the server-side product path is still less mature.

### Language support is finite

The deterministic code graph currently supports Go, JavaScript, TypeScript, Python, and Java in `backend/internal/codegraph/languages.go:21`.

Outside those languages, the system loses some of its strongest deterministic context.

## Final Assessment

Your agent is already a strong technical foundation for changed-code review.

The biggest gap is not "better prompting." The biggest gap is missing structured non-code context and missing deterministic coverage accounting.

If you fix those two areas first, the product will move from "smart AI reviewer" toward "credible PR assurance system."

In one sentence:

Today the system is good at reviewing code with context; next it needs to become good at reviewing intent, requirements, contracts, and complete PR coverage.
