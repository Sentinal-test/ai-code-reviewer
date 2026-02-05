# Plan: Accuracy & Context Optimization (v2)

**Goal**: Transform the AI Reviewer from a probabilistic "guesser" into a deterministic, structural analyzer using Code Graphs and PR Memory.

---

## 🎯 Core Objectives
1.  **Zero Guesswork**: Replace the Scout Pass (LLM-based) with deterministic static analysis (Tree-sitter).
2.  **Context Efficiency**: Send "code slices" (function bodies + signatures) instead of full files.
3.  **Institutional Memory**: Track resolved issues and regressions across commits in a single PR.

---

## 🏗️ Technical Architecture

### Phase 1: Deterministic Code Graph (Tree-sitter)
We will use **Tree-sitter** (language-agnostic parser) to build a structural map of the code BEFORE the LLM is called.

*   **Extraction**:
    *   **Symbols**: Identify definitions of functions, classes, and types in changed files.
    *   **Call Graph**: Map which functions call the modified code.
    *   **Context Slicing**: instead of `os.ReadFile(file)`, we extract `GetFunctionBody(file, symbolName)`.
*   **Benefits**:
    *   **Deterministic Callers**: If `func A` changes, we *know* `func B` calls it. No guessing.
    *   **Small Footprint**: Reduces context size by ~90% by sending only related snippets.

### Phase 2: PR Memory System (The "Institutional Knowledge")
Since GitHub Actions are stateless, we will implement a "Memory Layer" to track PR state.

*   **Storage**: 
    *   **Option A (CLI/Stateless)**: Use hidden markers in GitHub Comments to store state metadata.
    *   **Option B (Managed)**: A lightweight SQLite store (if using a persistent runner/volume).
*   **Features**:
    *   **Regression Detection**: "This nil-check was present in v1 but is missing in v3."
    *   **Deduplication**: Don't repeat the same comment if the code hasn't changed.
    *   **Context across commits**: Keep the Code Graph snapshot from the previous commit to detect breaking contract changes.

### Phase 3: Language-Agnostic Intelligence
We will prioritize features that work across all 40+ Tree-sitter supported languages:
*   **Signature Matching**: Verifying that a function call matches the updated signature in another file.
*   **Dependency Tracking**: Detecting if a new import adds a high-risk or deprecated library.

---

## 🚀 Execution Roadmap

### Stage 1: The "Smart Scout" (High ROI)
*   **Action**: Implement `internal/analysis` package in Go.
*   **Logic**:
    1.  Parse `diff` to find changed symbol names.
    2.  `git grep` to find files containing those symbols.
    3.  Extract minimal context around usages using Tree-sitter.
*   **Result**: 10x better accuracy for cross-file bugs.

### Stage 2: State Awareness (Memory)
*   **Action**: Update `PostReview` logic.
*   **Logic**:
    1.  Fetch existing workflow run metadata or PR comments.
    2.  Filter LLM suggestions against "already seen" or "already fixed" issues.
*   **Result**: Less noise, more trust.

### Stage 3: Professional Grading
*   **Action**: Integration of language-specific toolsets (e.g., `go/analysis` for Go, `ts-morph` for TS).
*   **Result**: Deep type-safety verification.

---

## 🏆 Success Metrics
| Metric | Current | Target |
| :--- | :--- | :--- |
| **False Positive Rate** | ~30% | < 10% |
| **Context Size (Tokens)** | 250k+ | < 50k |
| **Cross-file Detection** | Limited | Consistent |
| **Regression Catching** | No | Yes |
