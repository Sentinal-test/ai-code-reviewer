# Accuracy Acceleration Plan: Final Strategy

**Goal**: Transform the AI Code Reviewer into an enterprise-grade system that is language-independent, deterministic, and secure by design.

---

## 1. Language-Agnostic Context Engine
We will replace manual dependency resolution with a tree-sitter based engine to support any programming language.
*   **Tree-sitter Indexing**: Use Tree-sitter grammars to parse files locally on the runner.
*   **Feature Parity**: Whether it's Go, TypeScript, Python, or Rust, the engine will identify:
    *   **Modified Symbols**: Functions, methods, and types changed in the diff.
    *   **Type Graphs**: Local definitions of structs/interfaces used in the changes.
    *   **Call Sites**: Where the changed code is used to check for breaking changes.
*   **Security**: This indexing happens **locally** on the GitHub Runner. No code leaves the environment during this phase.

---

## 2. Multi-Agent Review Architecture
Instead of a single "black box" call, we use a coordinated panel of agents to reduce false positives.

| Agent | Focus | Tooling |
| :--- | :--- | :--- |
| **Logic Specialist** | Business logic, edge cases, state transitions | Gemini 2.0+ Flash (Context-rich) |
| **Security Sentinel** | OWASP vulnerabilities, secret leaks, auth flaws | Local Semgrep + Gemini Analysis |
| **Architect** | Structural consistency, design patterns | Local Repo Metadata + LLM |
| **Consolidator** | Conflict resolution and final JSON formatting | LLM Grader |

---

## 3. Large Context & Map-Reduce
To handle massive files and PRs without "forgetting" context:
1.  **Map Phase**: Split files into logical chunks (blocks/functions) locally using Tree-sitter.
2.  **Summary Generation**: Generate summaries for each chunk.
3.  **Reduce Phase**: Review the changed chunks while providing the summaries of unchanged sections as global context.

---

## 4. Security & Data Integrity
*   **Runner-Native Execution**: The core logic, indexing, and orchestration run entirely on the GitHub Action Runner.
*   **Secure LLM API**: Gemini (or other APIs) are used for inference. Code snippets are sent for review but not used for training or persistence (Enterprise policy).
*   **Analytics Export**: Only non-proprietary metadata (number of issues, severity, file paths) is exported for the dashboard. Actual source code remains on the runner.

---

## 5. Execution Roadmap

### Stage 1: Deterministic Pivot
*   Integrate `tree-sitter` for multi-language support.
*   Implement the local symbol-lookup engine.

### Stage 2: Agentic Loop
*   Implement the Multi-Agent panel (Logic, Security, Architect).
*   Implement the "Consolidator" to filter noise.

### Stage 3: Project Memory
*   Enable local caching of repo structure.
*   Implement "PR Memory" to track changes across multiple commits in the same PR.
