# AI Code Reviewer - Team Usage Guide

This guide details how to set up the **AI Code Reviewer** for any team member's repository.

## 🔑 1. Prerequisites (Secrets)

To use this action, your repository must have a Google Gemini API Key available.

1.  **Get an API Key**: [Google AI Studio](https://aistudio.google.com/app/apikey)
2.  **Add to Repository**:
    *   Go to your Repo -> **Settings** -> **Secrets and variables** -> **Actions**.
    *   Click **New repository secret**.
    *   Name: `GEMINI_API_KEY`
    *   Value: `AIza...` (your key)

## 📦 2. Usage (Choose your method)

Because your Action repository is **Private**, you have two ways to share it with your team.

### Option A: The "Private Repo" Method (Recommended for your Team)
Use this if you want to keep your code private. Teammates must use a **Personal Access Token (PAT)** to "download" the action into their workflow.

**Teammate Workflow:**
```yaml
name: AI Code Review
on:
  pull_request:
    types: [opened, synchronize]

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Your Code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Checkout AI Reviewer Action
        uses: actions/checkout@v4
        with:
          repository: tegveer-work/ai-code-reviewer
          token: ${{ secrets.ACTION_ACCESS_TOKEN }} # Their PAT with 'repo' scope
          path: .github/actions/ai-reviewer
          ref: main

      - name: Run AI Reviewer
        uses: ./.github/actions/ai-reviewer
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

---

### Option B: The "Standard" Method (If you make the repo PUBLIC)
If you make your repo public, anyone can use it with a single line. This is much cleaner.

**Teammate Workflow:**
```yaml
      - name: AI Code Reviewer
        uses: tegveer-work/ai-code-reviewer@main 
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

## ❓ FAQ

### Which one should I use?
*   **Use Option A** if you want to keep your AI Reviewer code hidden from the world.
*   **Use Option B** if you want the easiest setup and don't mind the code being public.

### "Dependencies file not found" error?
Ensure you are using the latest version of the action. The `action.yml` has been updated to handle paths correctly regardless of which option you choose!

