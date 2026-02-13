# Accuracy Acceleration Plan (v3): Deterministic Context & Multi-Agent Loops

**Goal**: Elevate AI Code Review accuracy to enterprise-grade by replacing probabilistic scouting with deterministic code intelligence and multi-agent coordination.

---

## 1. Deterministic Code Graph (Replacing the Scout Pass)
The "Scout Pass" (asking an LLM what it needs) is replaced by a **Structural Code Graph**.

### The Engine: SCIP + Tree-sitter
*   **Indexing**: We use **SCIP** (Symbolic Code Intelligence Protocol) or **Tree-sitter** to build a symbol index of the repository periodically (as a background job or during the first PR run).
*   **Symbol Extraction**: When a file changes, the system identifies all modified symbols (functions, structs, types).
*   **Dependency Resolution**:
    *   **Forward**: Find definitions of all symbols used in the diff.
    *   **Backward**: Find critical "callers" of the modified functions to check for breaking contract changes.
*   **Benefit**: 100% deterministic. The LLM receives every relevant definition it needs without "guessing."

---

## 2. Multi-Agent Review Architecture
Instead of one "Reviewer" LLM call, we use a coordinated **Panel of Agents**.

| Agent | Role | Focus |
| :--- | :--- | :--- |
| **Scout (Deterministic)** | Context Gathering | Queries the Code Graph for symbol definitions and cross-file dependencies. |
| **Logic Specialist** | Logic Review | Deep analysis of business logic, edge cases, and state changes. |
| **Security Sentinel** | Security Audit | Focuses on OWASP Top 10, sanitization, and permission flaws. |
| **Architect** | Structural Guard | Ensures the changes follow the project's ADR (Architectural Decision Records). |
| **Consolidator (Critic)** | final Grading | Resolves conflicting advice, removes false positives, and formats the final JSON. |

---

## 3. Large Context & Chunking Strategy
When a PR or a file exceeds the model's comfortable effective context window:

### A. Hierarchical Map-Reduce
1.  **Map**: Divide large files into functional chunks (using Tree-sitter to ensure chunk boundaries don't break function definitions).
2.  **Summarize**: Each chunk is reviewed for "local" bugs and summarized for "global" context.
3.  **Reduce**: The **Consolidator Agent** reviews the summaries and local bugs to find "global" inconsistencies (e.g., a variable initialized in Chunk A is misused in Chunk D).

### B. Sliding Window with Symbol Injection
*   Instead of a simple window, we use a **Sliding Window** of the diff, but always **inject the Global Type/Interface signatures** into every window. This ensures the model never forgets the "contracts" it is working within.

---

## 4. Institutional Project Memory
*   **Context Vector Store (RAG)**: We index the `docs/`, `ADRs/`, and `README.md` into a local vector store.
*   **Intent Injection**: Before any review, we query the store for: *"What are our rules for database transactions?"* or *"How do we handle error logging?"*
*   **Result**: The AI reviews code like a senior developer who has read every doc in the repo.

---

## 5. Execution Roadmap

### Stage 1: The Deterministic Pivot (Week 1-2)
*   Integrate `tree-sitter` for Go and TypeScript.
*   Replace `AnalyzeDependencyNeeds` with a symbol-lookup engine.
*   **Success**: 0% "Scout" API costs, 100% dependency coverage.

### Stage 2: The Agentic Loop (Week 3-4)
*   Implement the `Moderator` pattern to orchestrate multiple LLM calls.
*   Add the **Grader/Critic** step to filter noise.
*   **Success**: Drastic reduction in false positives.

### Stage 3: The Project Expert (Week 5+)
*   Enable RAG for project documentation.
*   Implement "PR Memory" to track issues across commits within the same PR.
*   **Success**: The AI catches regressions *added* during the PR review process itself.
