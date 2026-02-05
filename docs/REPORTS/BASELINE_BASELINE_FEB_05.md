# AI Reviewer: Baseline Accuracy Report (Feb 05, 2026)

This report establishes the baseline performance of the AI Code Reviewer using the **LLM-Only Scouting Strategy**. These results will be compared against future versions (e.g., Tree-sitter Discovery).

## 📈 Executive Summary

| Metric | Result |
| :--- | :--- |
| **Total Test Cases** | 9 |
| **Pass Rate (Recall=1.0)** | **67%** (6/9 cases) |
| **Avg. Bug Recall** | **70%** |
| **Avg. Precision** | **52%** (Includes valid non-intentional feedback) |
| **Avg. Discovery Recall** | **100%** (Low scale) |

---

## 🔍 Detailed Scenario Analysis

### 1. High-Precision Successes (Security/Logic)
The AI is highly effective at identifying well-defined, isolated defects.
*   **SQL Injection (`sql-injection`)**: 100% Recall. Perfectly identified unsanitized input.
*   **Hardcoded Secrets (`hardcoded-secret`)**: 100% Recall. Caught both DB URL and API keys.
*   **Nil Dereference (`case_001`)**: 100% Recall. Caught potential panic.

### 2. Context Discovery (Scout Pass)
*   **Cross-File Logic (`case_002`)**: 100% Discovery Recall. The AI correctly requested the database client file to verify the "Global Uniqueness" claim. However, precision was low (0.33) because it also gave several general tips.

### 3. Current Failures & Blind Spots
These are our primary areas for improvement in Phase 2.

#### ❌ Large File Stress Test (`large-file`) - **0% Recall**
*   **Failure**: The AI failed to find a hardcoded admin token buried at the bottom of a 1000+ line file.
*   **Root Cause**: Context distraction or "Lost in the Middle" phenomenon. 
*   **Fix Strategy**: Context Slicing (sending smaller chunks of large files instead of the whole thing).

#### ❌ Architectural Analysis (`god-function`) - **0% Recall**
*   **Failure**: Did not flag a massive 50+ line handler as a "God Function" violation despite it being its only job.
*   **Root Cause**: LLM was too focused on finding "bugs" (Nils/Security) rather than high-level structure.
*   **Fix Strategy**: Explicit "Structural Analysis" prompt phase.

#### ❌ Linting (`naming`) - **33% Recall**
*   **Failure**: Only found 1 out of 3 style violations.
*   **Note**: LLMs are often inconsistent with style. Deterministic linters (golangci-lint) should handle this, not the AI.

---

## 🛠️ Recommendations for Phase 2

1.  **Context Slicing**: Implement a strategy to break files >500 lines into relevant chunks based on the diff.
2.  **Structural Scout**: Use Tree-sitter to generate a "Symbol Map" instead of asking the LLM to guess dependencies.
3.  **Tiered Prompting**: Separate "Logic Analysis" from "Architecture Review" to prevent the model from ignoring structural issues.
