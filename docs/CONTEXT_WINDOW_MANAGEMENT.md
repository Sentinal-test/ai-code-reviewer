# Context Window Management

This document outlines how the AI Code Review system handles large codebases and PRs within the constraints of the LLM context window.

## Current Implementation

### Limits
- **Max Context Characters**: 3,500,000 (approx. 3.5MB of text).
- **Estimated Tokens**: ~875,000 to 1,000,000 tokens (assuming 1 token ≈ 3.5 - 4 characters for code).
- **Model Capacity**: Gemini 2.5 Flash supports up to 1M tokens.

### Priority-Based Truncation
When the total context (Changed Files + Dependencies + Repo Structure) exceeds 3,500,000 characters, the system applies a priority-based truncation strategy:
1. **Priority 1: Changed Files**: Full content of files in the PR with inline diff annotations.
2. **Priority 2: Dependencies**: Relevant files identified by the "Scout Pass".
3. **Priority 3: Repo Structure**: The overall file tree of the project.

Truncation occurs from Priority 3 downwards. If the Changed Files themselves exceed the limit, they are truncated using a UTF-8 safe mechanism to prevent JSON corruption.

---

## Future Scope: Retry & Chunking Mechanism

If the context window is reached and critical information is missing, the following "Multi-Pass" retry mechanism is planned:

### 1. Sliding Window Analysis
Instead of sending the whole file, the system will split large files into overlapping chunks (e.g., 5,000 lines per chunk with 500 lines overlap) and review them sequentially.

### 2. Recursive Summary Pass
If the dependency tree is too deep:
1. Pass 1: Summarize all dependency files.
2. Pass 2: Use summaries as context for the main review pass.

### 3. Priority Re-ranking
If truncation occurs, the system should log a warning. A retry can be triggered where the user (or system) manually selects a subset of "Most Critical" files to re-review without truncation.

## Token Estimation Table

| Feature | Approx. Characters | Approx. Tokens |
| :--- | :--- | :--- |
| Small PR (3 files) | 20,000 | 5,000 |
| Medium PR (15 files) | 150,000 | 40,000 |
| Large PR (50 files) | 600,000 | 150,000 |
| Repo Structure (Large) | 100,000 | 25,000 |
