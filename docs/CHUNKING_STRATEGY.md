# Token-Aware Chunking Strategy

## Why Chunking?

When a PR changes 50+ files, stuffing everything into a single LLM prompt causes:
- **Token overflow** → brute-force truncation drops important context
- **Diluted attention** → the LLM loses focus across too many unrelated changes
- **Missed cross-file bugs** → truncated deps mean the LLM can't trace call chains

Our solution: split large PRs into **graph-aware review chunks**, each with focused context.

---

## How It Works

```
PR (50 changed files)
  │
  ├─ Code Graph edges tell us which files are connected
  │
  ▼
┌────────────────────────────────────────┐
│  Graph-Aware Grouping (3-pass)         │
│                                        │
│  1. Build connected components:        │
│     auth/token.go ↔ api/middleware.go  │
│     (middleware calls ValidateToken)   │
│                                        │
│  2. Merge small components by dir      │
│                                        │
│  3. Pack into chunks by token budget   │
└────────────────────────────────────────┘
  │
  ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│   Chunk 1    │  │   Chunk 2    │  │   Chunk 3    │
│ auth + api   │  │ cmd/cli      │  │ internal/db  │
│ (connected)  │  │ + llm/       │  │ + models/    │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │                 │                 │
       ▼                 ▼                 ▼
  LLM Review 1     LLM Review 2     LLM Review 3
  (scoped deps)    (scoped deps)    (scoped deps)
       │                 │                 │
       └────────┬────────┘────────┬────────┘
                ▼                 
         Consolidator             
    (deduplicate, merge)          
                │                 
                ▼                 
        Final ReviewResult        
```

---

## Key Design: Graph-Aware Grouping

**Naive approach** (directory-only):
```
Chunk 1: pkg/auth/token.go
Chunk 2: pkg/api/middleware.go
→ Neither chunk sees that middleware.go calls ValidateToken from token.go
→ Cross-boundary bug MISSED
```

**Our approach** (graph-aware):
```
Code graph edge: middleware.go → token.go (via ValidateToken)
→ Both files placed in the SAME chunk
→ LLM sees the full auth + API change together
→ Cross-boundary bug CAUGHT
```

The code graph's `Edge` data tells us exactly which files depend on each other. We use this to build **connected components** — groups of files linked by symbol references — and keep them together in the same chunk.

---

## Token Budget

| Component | Budget |
|:---|:---|
| System prompt + instructions | ~5K tokens |
| PR context (title, commits) | ~2K tokens |
| Repo structure | ~5K tokens |
| Code graph deps (scoped) | ~20K tokens |
| **Actual code (diff hunks)** | **~168K tokens** |
| **Total per chunk** | **~200K tokens** |

Gemini 2.5 Pro supports an industry-leading 2M token context window. We target 200K per chunk for a massive safety margin, ensuring the model's attention span remains razor-sharp in evaluating the code without context dilution.

---

## What Each Chunk's Prompt Contains

1. **Diff hunks** — only for this chunk's files (not all PR files)
2. **File headers** — import/package context for this chunk's files
3. **Scoped dependencies** — code graph snippets relevant to THIS chunk only
4. **Context graph** — full cross-file relationship map (compact, ~2K)
5. **Cross-chunk refs** — brief list of files in OTHER chunks that this chunk depends on, so the LLM can flag architectural issues

---

## Intelligent Consolidation

Results from all chunks and all 3 specialist agents (Correctness, Security, Structure) are passed to the **Consolidator** in a final, low-cost LLM pass:

- **Deduplication**: Semantically similar comments (e.g., both Security and Correctness flagging the same missing validation) are merged intelligently.
- **Summary merging**: Per-chunk summaries are compiled into a single high-level review overview.
- **Cost Efficiency**: This pass runs on the much cheaper `Gemini 2.5 Flash` model, as it only processes text outputs and does not require complex codebase reasoning.
- **Fallback**: If the LLM consolidator fails, the system automatically falls back to a deterministic grouping strategy.

---

## Small PRs: Zero Overhead

If the entire PR fits within the 200K token budget (most PRs do), it becomes a **single chunk** — identical to today's flow. No extra calls, no chunking overhead.

---

## Files

| File | Description |
|:---|:---|
| `internal/chunker/chunker.go` | Token estimation, graph-aware file grouping |
| `internal/chunker/chunker_test.go` | Unit tests for grouping logic |
| `internal/llm/consolidator.go` | Intelligent LLM-based `RunConsolidation` |
| `internal/orchestrator/orchestrator.go` | Multi-agent chunk orchestration |
