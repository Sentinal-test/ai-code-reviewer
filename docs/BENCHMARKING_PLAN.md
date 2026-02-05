# AI Code Reviewer Benchmarking Plan

This document defines how we measure "Accuracy" beyond just feelings. We use a **Scientific Data-Driven Approach** to evaluate how well the AI performs against a "Sacred Truth" (Ground Truth).

## 📊 Metrics Glossary

To understand the benchmark output, here are the key terms:

| Metric | Definition | Why it matters |
| :--- | :--- | :--- |
| **Recall** | % of "Sacred Bugs" identified by the AI. | **Completeness**. Does the AI miss critical bugs? |
| **Precision** | % of AI comments that were actually about our defined bugs. | **Signal-to-Noise**. Does the AI spam the dev with minor/invalid tips? |
| **DepRecall** | % of required dependency files found during the Scout Pass. | **Context Efficiency**. Can the AI find what it needs? |
| **Pass/Fail** | A case **PASSES** only if **Recall = 1.0** (found all core bugs). | **Reliability**. High recall is our #1 requirement. |

### The "Precision" Nuance
> [!NOTE]
> Precision in this benchmark is often lower than 1.0. This is because the AI might find *other* valid issues (linting, minor style) that we didn't include in the `truth.json`. We focus on **Recall** as our primary success metric.

## 🧪 Strategy Comparison (A/B Testing)

Our benchmark is designed to be **plug-and-play**. By keeping the **Test Cases** constant, we can swap internal components and measure the delta in performance.

### Experiment 1: Scout Pass Evolution
We will compare two different implementations of the "Discovery" layer:

1.  **Baseline (Current)**: Probabilistic LLM-based Scout (`llm.AnalyzeDependencyNeeds`).
2.  **Challenger (Proposed)**: Deterministic Code Graph Scout (using Tree-sitter).

**What to watch**:
*   If **DepRecall** increases from 0.70 to 1.00, we've solved the "Context Gap".
*   If **Overall Recall** increases alongside it, it proves that providing the right context was the blocker for the AI.

### Experiment 2: Review Logic Evolution
We can also test different LLM models or prompt strategies by keeping the Scout fixed and swapping the `RunReview` function.

---

## 1. Benchmarking Workflow

### Key Performance Indicators (KPIs)

To prove that a context strategy (like Tree-sitter) is better than a baseline, we must measure:

| Metric | Definition | Goal |
| :--- | :--- | :--- |
| **Precision** | % of AI comments that are actually bugs (not noise). | Higher = Less Noise |
| **Recall** | % of real bugs in the code that the AI successfully found. | Higher = More Secure |
| **False Positive Rate**| % of non-bug code flagged as problematic. | Lower = Less developer fatigue |
| **Token Efficiency** | `(TP + TN) / Total Tokens Used`. | Higher = Cheaper / Better context selection |
| **Context Hit Rate** | % of times the required dependency was actually included in context. | Higher = Better Scout Pass |

## 2. Canonical Dataset Format

Every test case must follow this exact structure to ensure the evaluation oracle can parse it automatically.

### 📁 Folder Structure
```text
datasets/
└── <language>/
    └── <bug-category>/
        └── <case_id>/
            ├── buggy/          # Code with the intentional bug
            ├── fixed/          # Code with the fix (for reference)
            ├── diff.patch      # The diff generated between buggy and fixed
            └── truth.json      # The Ground Truth (sacred)
```

### 📄 `truth.json` (Full-Stack Oracle)
This file now defines the expected behavior for the *entire* system logic.

```json
{
  "id": "GO-CROSS-002",
  "language": "go",
  "category": "cross-file-logic",
  "pr_context": {
    "title": "Fix user creation logic",
    "body": "Ensuring we don't allow duplicate emails.",
    "commits": ["feat: user validation", "fix: check email"]
  },
  "expected_dependencies": [
    "internal/db/client.go"
  ],
  "bugs": [
    {
      "type": "logic",
      "severity": "high",
      "file": "service.go",
      "line": 47,
      "description": "Email uniqueness is checked against local cache, not DB. Potential race/bypass."
    }
  ],
  "expected_comments": 1
}
```

## 3. Advanced Evaluation Scenarios

### A. Scout Pass Validation
*   **Goal**: Ensure the AI correctly identifies which files to fetch.
*   **Metric**: `Recall (Dependencies)` = `Found Deps / Expected Deps`.

### B. PR Context Sensitivity
*   **Goal**: Ensure the AI ignores "Intentional Debugging" if the PR Body says "added logs for debugging".
*   **Test Case**: A "buggy" file with verbose logging that should NOT be flagged if the PR body mentions it.

### C. Large Context Stress Test
*   **Goal**: Measure accuracy when the `repoStructure` contains 10,000+ files (simulated).
*   **Metric**: `Accuracy vs Noise` under high context load.

## 4. Scenarios for Dependency Benchmarking

To prove our "Smart Scout" strategy is effective, we need test cases where the bug is invisible *unless* specific dependency files are provided.

| Scenario | Case Folder | Expected Scout Fetch | Why it's a test |
| :--- | :--- | :--- | :--- |
| **Type Integrity** | `dependency/type-info` | `models/user.go` | Change uses `user.Email`, but the field was renamed to `user.EmailAddr`. LLM needs the struct def to see the typo. |
| **Logic Leak** | `dependency/logic-leak` | `pkg/auth/validator.go` | Change calls `validator.Verify()`. Without the source, LLM doesn't know `Verify` throws a specific error that isn't handled. |
| **Const Check** | `dependency/const-val` | `internal/limits.go` | Change sets `MaxRows = 1000`. Dependency defines `HardLimit = 500`. Without fetch, LLM misses the limit violation. |
| **Interface Gap** | `dependency/interface` | `gateway/provider.go` | Change implements a method but with wrong signature from the interface definition. |

## 5. Execution Roadmap

### A. Synthetic/Public Datasets (Baseline)
Use existing benchmarks to test specific layers:
*   **Security (OWASP Benchmark)**: A large collection of Java code with known vulnerabilities.
*   **Go-specific**: [Gosec Benchmarks](https://github.com/securego/gosec) or common Go CVE-reproduction repos.
*   **LLM-specific**: [HumanEval](https://github.com/openai/human-eval) (for logic) or [SWE-bench](https://www.swebench.com/) (for real-world PR resolution).

### B. Internal "Gold Standard" Dataset (Best for Accuracy)
1.  Collect 50-100 historical PRs from your team where a bug was *actually* found during human peer review.
2.  Create a "Ground Truth" file for each PR:
    *   `PR #123`: Bug at Line 45 of `main.go`. Severity: High. Layer: Security.
3.  Run the AI Reviewer on these PRs.
4.  Compare results:
    *   Did it find the bug at Line 45? (TP)
    *   Did it flag Line 60 which is fine? (FP)
    *   Did it skip the bug? (FN)

## 3. Benchmarking Workflow

### Phase 1: Automated Eval Tool (1-2 weeks)
Create a script `scripts/benchmark.go` that:
1.  Iterates through a folder of test cases (diff + expected comments).
2.  Runs the AI Reviewer CLI for each.
3.  Calculates the **F1 Score** (Balance of Precision and Recall).

### Phase 2: A/B Testing Context Strategies
Run the same benchmark twice:
*   **Run A**: Old strategy (Full file guessing).
*   **Run B**: New strategy (Deterministic Tree-sitter discovery).
*   **Compare**: If Run B has higher Recall with lower Token Usage, we have scientific proof of improvement.

### Phase 3: Qualitative Human Review
For all "False Positives" flagged by AI, have a senior lead review them. Sometimes the AI is "Right" about a code smell even if it's not a "Bug". These are converted to "Expert Validated" results.

## 4. Next Steps
1.  **Select 5 PRs** that had real bugs as your initial "Unit Test" suite.
2.  **Run the CLI** manually on them and record the results in a spreadsheet.
3.  **Implement Stage 1 of the Accuracy Plan** and re-run.
