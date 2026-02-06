# Cost Analysis (AI Code Reviewer)

This document provides a detailed financial breakdown for operating the AI Code Review system using **Google Gemini 2.5 Flash**.

## Pricing Model (Gemini 2.5 Flash)

Google's latest efficient model offers high performance at a significantly lower cost point.

| Model | Input (per 1M tokens) | Output (per 1M tokens) | INR Cost (₹90 per $1) |
| :--- | :--- | :--- | :--- |
| **Gemini 2.5 Flash** | $0.10 | $0.40 | ₹9.00 - ₹36.00 |

> [!NOTE]
> Pricing is calculated based on standard pay-as-you-go rates. Context caching (planned) can further reduce input costs by up to 50% for repetitive codebases.

---

## Token-to-Line Conversion (Heuristics)

To make costs more relatable, we use the following standard conversion for source code:
**1 Line of Code (LOC) ≈ 12 Tokens (Average)**

| Code Volume | Approx. Tokens | USD Cost (Input) | INR Cost (Input) |
| :--- | :--- | :--- | :--- |
| 100 Lines | 1,200 | $0.00012 | ₹0.01 |
| 1,000 Lines | 12,000 | $0.0012 | ₹0.11 |
| 10,000 Lines | 120,000 | $0.012 | ₹1.08 |
| 50,000 Lines | 600,000 | $0.06 | ₹5.40 |

---

## Understanding "Total Context"

The cost of a review is determined by the "Total Context" sent to the LLM. This is **not limited to changed lines**; it is a cumulative package that includes:

| Context Component | Description | Avg. Token Weight |
| :--- | :--- | :--- |
| **System Prompt** | Core analysis rules and defect detection logic. | 2,000 - 3,000 |
| **Repo Structure** | Complete file tree of the project for architecture context. | 1,000 - 5,000 |
| **PR Metadata** | Title, description, and recent commit messages. | 500 - 1,500 |
| **Git Diff** | The raw code changes being reviewed. | Variable |
| **Changed Files** | Entire content of modified files (to provide full context). | High |
| **Scout Dependencies** | External files identified by the Scout Pass as relevant. | Variable |

---

## Two-Pass Analysis Architecture (Additive Model)

The system operates in two distinct phases. Note that the **Review Pass** context is significantly larger because it inherits the findings from the Scout Pass.

1.  **Scout Pass (Context Gathering)**:
    - Scans the Diff + Repo Structure + Metadata.
    - **Goal**: Identify which 5-10 external files (dependencies/interfaces) are needed for a perfect review.
2.  **Review Pass (Deep Analysis)**:
    - Scans everything from the Scout Pass **PLUS** the full content of all identified dependencies.
    - **Goal**: Generate high-precision feedback based on complete cross-file understanding.

---

## Cost Scenarios (Per Pull Request)

These scenarios reflect the **inclusive** nature of the context (Prompts + Metadata + Files).

### Scenario A: Small PR (Hotfix/Refactor)
*   **Description**: 1-3 files changed, minimal dependencies.
*   **Total Context Volume**: ~15,000 Tokens (~1,250 Lines)
*   **Breakdown**:
    *   **Scout**: 5k In (Prompts + Structure + Diff)
    *   **Review**: 10k In (Scout Context + Full Modified Files)
*   **Cost**: $0.0015 (In) + $0.00024 (Out) = **$0.00174 (₹0.16)**

### Scenario B: Medium PR (New Feature)
*   **Description**: 5-15 files changed, requires cross-file type validation.
*   **Total Context Volume**: ~80,000 Tokens (~6,600 Lines)
*   **Breakdown**:
    *   **Scout**: 20k In (Larger Structure + Complex Diff)
    *   **Review**: 60k In (Scout Context + 8-10 Dependency Files identified by Scout)
*   **Cost**: $0.008 (In) + $0.00048 (Out) = **$0.00848 (₹0.76)**

### Scenario C: Large PR (Architecture Update)
*   **Description**: 30+ files, heavy framework utilization.
*   **Total Context Volume**: ~400,000 Tokens (~33,000 Lines)
*   **Breakdown**:
    *   **Scout**: 100k In (Extensive structure + Deep metadata)
    *   **Review**: 300k In (Scout Context + Full System Specs + Heavy Dependencies)
*   **Cost**: $0.04 (In) + $0.0012 (Out) = **$0.0412 (₹3.71)**

---

## Estimated Monthly Budget

Based on a team of **10 developers** performing **1000 PRs per month**.

| PR Complexity | Volume (Mix) | Cost per PR (INR) | Monthly Total (INR) |
| :--- | :--- | :--- | :--- |
| **Small** | 600 (60%) | ₹0.16 | ₹96 |
| **Medium** | 300 (30%) | ₹0.76 | ₹228 |
| **Large** | 100 (10%) | ₹3.71 | ₹371 |
| **TOTAL** | **1000 PRs** | -- | **₹695** |

> [!IMPORTANT]
> **Operational Efficiency**: The total cost to support a 10-person dev team is approximately **₹700 per month**. This covers the entire system overhead, including dependency analysis and full repository context mapping.
