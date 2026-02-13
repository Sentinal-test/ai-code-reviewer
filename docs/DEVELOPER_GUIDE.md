# AI Code Reviewer (Sentinel) - Developer Setup Guide

Enable industry-grade AI code reviews on your repository.

## One-Minute Setup

### 1. Install the GitHub App
First, you must authorize our reviewer to access your repository.

👉 **[Install Sentinel Review App](https://github.com/settings/apps/sentinal-review/installations)**

*   Select "Only select repositories" (Recommended) or "All repositories".
*   Click **Install**.

### 2. Configure Repository Secret
Each repository needs its own Gemini API key for analysis.

1.  **Get an API Key**: [Google AI Studio](https://aistudio.google.com/app/apikey)
2.  **Add to GitHub**:
    *   Go to your Repository -> **Settings** -> **Secrets and variables** -> **Actions**.
    *   Click **New repository secret**.
    *   Name: `GEMINI_API_KEY`
    *   Value: `AIza...` (your key)

### 3. Add the Workflow File
Create a file at `.github/workflows/ai-review.yml` in your repo and paste the following:

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

**That’s it!** Every time you open or update a PR, the AI will automatically analyze your code and post review comments.

---

## 💡 Why use the App flow?

*   ✅ **Simple Setup**: One App installation and one YAML file.
*   ✅ **Team Standard**: Uses our organization’s fine-tuned review logic.
*   ✅ **Instant Removal**: Grant or revoke access anytime from GitHub settings.
*   ✅ **Least Privilege**: The app only sees what it needs to review the PR.

## ❓ Need Help?
If the review doesn't start, ensure:
1.  The App is installed on the specific repository.
2.  The repository is part of our approved organization.

<a name="troubleshooting"></a>
## 🛠️ Troubleshooting

### 1. `Error: Unable to resolve action... repository not found`
**Reason**: Your AI Reviewer repository is **Private**, and the repository you are reviewing can't see it.
**Fix**: 
- Use **Option A (Private Action)** from the README.
- You must checkout the AI Reviewer code into your workflow using `actions/checkout@v4` with a Personal Access Token (`ACTION_ACCESS_TOKEN`).

### 2. `❌ Error: GEMINI_API_KEY is required`
**Reason**: The `GEMINI_API_KEY` secret is not being passed correctly to the action.
**Fix**: 
- Ensure you have added `GEMINI_API_KEY` to **Settings > Secrets and variables > Actions** in the repository being reviewed.
- Check that your YAML file includes:
  ```yaml
  with:
    gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
  ```

### 3. `Warning: Restore cache failed`
**Reason**: This is often a non-fatal warning from `actions/setup-go` on first run.
**Fix**: Safe to ignore if the build completes and the review runs.

