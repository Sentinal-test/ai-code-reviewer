# Cost Analysis & Unit Economics (AI Code Reviewer)

This document provides a detailed financial breakdown and architectural cost mechanics for operating the AI Code Review system in production.

## The Hybrid Pricing Model

To balance world-class reasoning with financial efficiency, the system employs a **Hybrid LLM Architecture**:

| Agent Type | Model Used | Input Cost (per 1M tokens) | Output Cost (per 1M tokens) | Purpose |
| :--- | :--- | :--- | :--- | :--- |
| **3x Specialists** (Correctness, Security, Structure) | **Gemini 2.5 Pro** | $1.25 (~₹112) | $5.00 (~₹450) | High-end reasoning, bug hunting, and architectural analysis. |
| **1x Consolidator** | **Gemini 2.5 Flash** | $0.075 (~₹6.75) | $0.30 (~₹27) | Fast, cheap semantic deduplication and JSON formatting. |

---

## Cost Multipliers (The Mechanics of the Bill)

Unlike standard chatbots, agentic code review is highly iterative. Three specific architecture choices act as **Cost Multipliers**:

### 1. The Multi-Agent Multiplier (3x)
Every PR chunk is sent simultaneously to three independent Pro agents. 
*A 10,000-token prompt immediately becomes 30,000 billed input tokens.*

### 2. The Tool-Call Loop Multiplier (1x - 8x)
When an agent decides to use a tool (like `get_callers`), it enters a reasoning loop. To preserve the chain-of-thought, the *entire previous conversation history* (including the massive initial prompt containing the diff and repo structure) is sent back to the LLM on every turn.
*If an agent takes 4 tool calls to find a bug, a 20,000-token prompt is billed 5 times = 100,000 input tokens.* Let the loop iteration count be $N$. Cost $= N \times Context$.

### 3. The Chunking Multiplier
To prevent the LLM from losing "attention" in massive PRs, files are split into overlapping chunks (max ~20 files/chunk).
*A large 60-file PR split into 3 chunks results in 9 total Pro agent instances launched.*

---

## Cost Mitigation Strategies (How we save money)

Despite the multipliers, the system aggressively trims token waste:

| Strategy | Token Saving | Description |
| :--- | :--- | :--- |
| **Code Graph (Slim Deps)** | **~90%** | Instead of injecting full dependency files, we send a compact symbol index (signatures only). Agents fetch full code via tools *only* if needed. |
| **Diff-Only Mode** | **Critical** | For massive files, we only send imports + ±50 lines around changes, preventing "Out of Memory" errors and massive bills on auto-generated code. |
| **Flash Consolidation** | **~95%** | Passing 3 agent outputs to 2.5 Pro for merging would unnecessarily burn Pro tokens. Using Flash for this step drops the deduplication cost to fractions of a penny. |
| **Tool Opt-Out** | **Variable** | For simple PRs (<50 lines), agents are instructed that tool usage is *optional*, allowing them to 1-shot the review and avoid the Tool-Call Loop multiplier. |

---

## Cost Scenarios (Per Pull Request)

Prices below are calculated using the prevailing **Gemini 2.5 Pro** rates ($1.25/1M input).

### Scenario A: Small PR (Hotfix/Minor Change)
*   **Description**: < 5 files changed, minimal dependencies. Small enough for 1-shot reviews.
*   **Tokens/Execution**: ~6k context * 3 agents * ~1.5 tool loops = **~27,000 Input Tokens**
*   **Total Cost**: **~$0.04 (₹3.50) per PR**

### Scenario B: Medium PR (New Feature)
*   **Description**: 5-15 files, complex cross-file logic requiring investigation.
*   **Tokens/Execution**: ~20k context * 3 agents * ~4 tool loops = **~240,000 Input Tokens**
*   **Total Cost**: **~$0.31 (₹28.00) per PR**

### Scenario C: Large / Oversized PR (Architecture Shift)
*   **Description**: 30+ files, massive refactoring.
*   **Tokens/Execution**: Split into 3 chunks. 3 chunks * (40k context * 3 agents * 5 tool loops) = **~1.8M Input Tokens**
*   **Total Cost**: **~$2.25 (₹200.00) per PR**

---

## Monthly Budget Projection

Based on a standard engineering team of **10 developers** performing **1,000 PRs per month** (Mix: 60% Small, 30% Medium, 10% Large).

| PR Mix | Count | Avg Cost/PR (USD) | Avg Cost/PR (INR) | Monthly Total (USD) | Monthly Total (INR) |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Small** | 600 | $0.04 | ₹3.50 | $24.00 | ₹2,100 |
| **Medium** | 300 | $0.31 | ₹28.00 | $93.00 | ₹8,400 |
| **Large** | 100 | $2.25 | ₹200.00 | $225.00 | ₹20,000 |
| **TOTAL** | **1,000 PRs** | -- | -- | **$342.00** | **₹30,500** |

> [!IMPORTANT]
> **Operational ROI**: Supporting a 10-person dev team costs approximately **$350 (₹30,000) per month**. Compared to the combined hourly rate of senior engineers manually hunting for subtle state leaks, architectural drift, or security vulnerabilities, the system pays for itself in just hours of human-time saved per month.
