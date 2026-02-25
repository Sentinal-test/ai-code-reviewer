# Cost Analysis (AI Code Reviewer)

This document provides a detailed financial breakdown for operating the AI Code Review system using **Google Gemini 1.5/2.0 Flash**.

## Pricing Model (Gemini Flash)

The system is optimized for "Flash" models, which provide a balance of deep reasoning and cost efficiency.

| Model | Input (per 1M tokens) | Output (per 1M tokens) | INR Cost (₹90 per $1) |
| :--- | :--- | :--- | :--- |
| **Gemini 1.5 Flash** | $0.075 | $0.30 | ₹6.75 - ₹27.00 |
| **Gemini 2.0 Flash** | $0.10 | $0.40 | ₹9.00 - ₹36.00 |

---

## The Efficiency Stack (How we save tokens)

Unlike simple review tools, this system uses several optimization layers to keep costs low even for complex PRs.

| Strategy | Token Saving | Description |
| :--- | :--- | :--- |
| **Code Graph (Slim Deps)** | **~90%** | Instead of full files, we send a compact symbol index. Agents only fetch code via tools IF needed. |
| **Diff-Only Mode** | **Critical** | For files >180k tokens, we only send imports + ±50 lines around changes, preventing "Out of Memory" errors and massive bills. |
| **Smart Chunking** | **~30%** | Groups related files together to avoid redundant system prompt overhead. |

---

## Multi-Agent Cost Multiplier

The system uses a **Parallel Specialist Architecture**. Every review involves 4 LLM calls:
1.  **🐛 Correctness Agent**: Focuses on bugs and performance.
2.  **🔐 Security Agent**: Focuses on vulnerabilities and secrets.
3.  **🏗️ Structure Agent**: Focuses on patterns and architecture.
4.  **🔄 Consolidator**: Intelligent LLM merge to remove noise and false positives.

**Heuristic**: Total PR Cost = `(3 * Specialist_Context) + Consolidator_Context`.

---

## Cost Scenarios (Per Pull Request)

Prices below are calculated using **Gemini 2.0 Flash** rates (higher bound).

### Scenario A: Small PR (Hotfix/Minor Change)
*   **Description**: < 5 files changed, minimal dependencies.
*   **Context Volume**: ~5k tokens per agent.
*   **Total Cost**: **₹0.20 per PR**

### Scenario B: Medium PR (New Feature)
*   **Description**: 5-15 files, cross-file logic.
*   **Context Volume**: ~20k tokens per agent + Code Graph usage.
*   **Total Cost**: **₹1.80 per PR**

### Scenario C: Large / Oversized PR (Architecture Shift)
*   **Description**: 30+ files or massive generated files.
*   **Context Volume**: ~100k tokens (capped by Diff-Only mode).
*   **Total Cost**: **₹8.50 per PR**

---

## Monthly Budget Projection

Based on a team of **10 developers** performing **1,000 PRs per month** (Mix: 60% Small, 30% Medium, 10% Large).

| PR Mix | Count | Cost per PR (INR) | Monthly Total (INR) |
| :--- | :--- | :--- | :--- |
| **Small** | 600 | ₹0.20 | ₹120 |
| **Medium** | 300 | ₹1.80 | ₹540 |
| **Large** | 100 | ₹8.50 | ₹850 |
| **TOTAL** | **1,000 PRs** | -- | **₹1,510** |

> [!IMPORTANT]
> **Operational ROI**: Supporting a 10-person dev team costs approximately **₹1,500/month**. Compared to the manual time spent catching duplicate bugs or security flaws, the system pays for itself in just minutes of human-time saved.
