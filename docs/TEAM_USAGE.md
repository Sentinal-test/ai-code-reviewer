# AI Code Reviewer - Team Usage Guide

This guide explains how any team or repository can start using the AI Code Reviewer.

## 🚀 The Best Practice (GitHub App + Actions)

This is the recommended setup. It uses a GitHub App for secure authentication and GitHub Actions for the review process.

### 1. Requirements
*   **Gemini API Key**: Each repository must have a `GEMINI_API_KEY` secret.
    *   Go to **Settings > Secrets and variables > Actions**.
    *   Create a new secret named `GEMINI_API_KEY`.
*   **App Installation**: The Sentinel Review App must be installed on the repository.
    *   👉 **[Install Sentinel Review App](https://github.com/settings/apps/sentinal-review/installations)**

### 2. Workflow Setup
Add the following to `.github/workflows/ai-review.yml`:

```yaml
name: AI Code Review
on:
  pull_request:
    types: [opened, synchronize]

permissions:
  contents: read
  pull-requests: write

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Run AI Reviewer
        uses: tegveer-work/ai-code-reviewer@main 
        env:
          GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}
```

## 💡 Key Features of this Setup

*   **Diff Context**: Automatically fetches related files (dependencies) using a preliminary "Scout" LLM pass.
*   **Privacy**: Your code never leaves the GitHub environment except for the LLM analysis via encrypted API calls.
*   **Zero Maintenance**: Updates to the reviewer logic are automatically applied when we push to the `main` branch.

## ❓ Troubleshooting

### No comments appearing?
1. Check the **Actions** tab in your repo to see if the workflow failed.
2. Ensure the `GEMINI_API_KEY` is valid and hasn't expired.
3. Verify that the GitHub App is installed on the repo.

### "Not Found" error during checkout?
Ensure the repository `tegveer-work/ai-code-reviewer` is public, or that you have added the necessary tokens if it's private.
