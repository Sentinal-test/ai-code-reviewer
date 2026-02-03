# Cost Analysis (AI Code Reviewer)

This document provides a financial breakdown of operating the AI Code Review system using Google Gemini models. 

## Pricing Model (Gemini 1.5)

*Note: Prices are estimates based on standard pay-as-you-go pricing (USD) and converted to INR at 1 USD = 83 INR.*

| Model | Input (per 1M tokens) | Output (per 1M tokens) | INR (approx. per 1M tokens) |
| :--- | :--- | :--- | :--- |
| **Gemini 1.5 Flash** | $0.075 | $0.30 | ₹6.23 - ₹24.90 |
| **Gemini 1.5 Pro** | $1.250 | $5.00 | ₹103.75 - ₹415.00 |

*Note: Pricing doubles for context windows larger than 128k tokens.*

---

## Cost Scenarios (Per Pull Request)

Assuming Gemini 1.5 Flash is used for standard reviews.

### Scenario A: Small PR (Hotfix)
*   **Description**: 1-3 files changed.
*   **Context usage**: ~10k tokens input, 500 tokens output.
*   **Cost**: $0.00075 + $0.00015 = $0.0009
*   **INR Cost**: **₹0.075 (Less than 10 paise)**

### Scenario B: Medium PR (Feature)
*   **Description**: 10-15 files changed, standard dependencies.
*   **Context usage**: ~50k tokens input, 1k tokens output.
*   **Cost**: $0.00375 + $0.0003 = $0.00405
*   **INR Cost**: **₹0.34 (34 paise)**

### Scenario C: Large PR (Architecture Change)
*   **Description**: 40+ files, heavy context utilization.
*   **Context usage**: ~250k tokens input, 2k tokens output.
*   *Pricing doubles (>128k context)*.
*   **Cost**: $(250k * 0.15/1M) + (2k * 0.60/1M) = $0.0387
*   **INR Cost**: **₹3.21**

---

## Monthly Budget (Stakeholder Estimate)

Based on a team of 10 developers performing 5 PRs per day (approx 1000 PRs/month):

| Component | Usage Mix | Monthly Cost (USD) | Monthly Cost (INR) |
| :--- | :--- | :--- | :--- |
| 600 Small PRs | 60% | $0.54 | ₹45 |
| 300 Medium PRs | 30% | $1.22 | ₹101 |
| 100 Large PRs | 10% | $3.87 | ₹321 |
| **TOTAL** | **1000 PRs** | **$5.63** | **₹467**

> [!IMPORTANT]
> **Total Monthly Operational Cost: ~₹470 - ₹500**
> This covers a team of 10 developers. The cost is extremely efficient compared to manual review time or expensive enterprise tools.

---

## Cost Optimization Strategies
1. **Filtering**: Current system already filters documentation and config files (`.md`, `.gitignore`, `package-lock.json`), reducing input costs by 15-20%.
2. **Layer Selection**: Users can toggle review layers (Security, Performance, etc.) via the dashboard to control depth vs cost.
3. **Caching**: We plan to implement context caching for repeat PR updates to reduce input costs by up to 50%.
