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

## 📦 2. Usage (Copy-Paste)

Create a new file in your repository at `.github/workflows/ai-review.yml`.

Copy and paste the following content. **Make sure to update the `uses` version tag**.

```yaml
name: AI Code Review

on:
  pull_request:
    types: [opened, synchronize]

permissions:
  contents: read
  pull-requests: write # Required to post comments

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0 # IMPORTANT: Required to correct diffs

      - name: AI Code Reviewer
        # 👇 UPDATE THIS LINE:
        # uses: your-username/ai-code-reviewer@v1
        uses: tegveer-work/ai-code-reviewer@v1 
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

## ❓ FAQ

### "Action not found" error?
Ensure you are using the correct repository name in the `uses:` line. It should be `your-username/repo-name@tag`.

### "Dependencies file not found" error?
This has been fixed in `v1`. Ensure you are pointing to the latest version of the action where the `go.sum` path logic was corrected.

### "Repository not found" (Private Repos)?
If the Action repository is **Private**, other repositories cannot access it by default.
*   **Solution**: Make the Action repository **Public**.
*   **Alternative for Enterprise**: If within the same Organization, check "Allow access to components in this organization".
