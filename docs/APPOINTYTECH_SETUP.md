# AI Code Review Agent Setup (Appointytech Organization)

This guide explains how developers in the **appointytech** organization can enable the AI Code Reviewer on their repositories.

## ⚙️ Setup Steps

1. **Go to your repository** on GitHub.
2. Navigate to the `.github/workflows` directory (create it if it doesn't exist).
3. **Create a new file** named: `ai-review.yml`
4. **Paste the following YAML** into the file:

```yaml
name: AI Code Review

on:
  pull_request:
    types: [opened, synchronize]

permissions:
  contents: read
  pull-requests: write
  checks: write

jobs:
  review:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout Code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Run AI Reviewer
        uses: appointytech/ai-code-reviewer@main
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
          github_app_id: ${{ secrets.AI_REVIEWER_APP_ID }}
          github_private_key: ${{ secrets.AI_REVIEWER_PRIVATE_KEY }}
```

## 🔒 Secrets

**You do not need to configure any secrets manually.** 
All required secrets (`GEMINI_API_KEY`, `AI_REVIEWER_APP_ID`, `AI_REVIEWER_PRIVATE_KEY`) are already set up at the **appointytech organization level** and will be automatically available to your repository's workflow.

## 🌿 Branch Requirements

To ensure the AI Code Reviewer runs automatically on all Pull Requests:
* The `.github/workflows/ai-review.yml` file **must be merged into your repository's default/target branch** (usually `main` or `master`). 
* Once the workflow file is present on the target branch, any new Pull Requests or updates (synchronize) to existing Pull Requests will trigger the AI code review automatically.

---
*If you have any issues with the setup, please reach out to the platform/DevOps team.*