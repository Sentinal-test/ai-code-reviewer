# AI Code Reviewer (Sentinel) - Developer Setup Guide

Enable industry-grade AI code reviews on your repository.

## One-Minute Setup

### 1. Configure Repository Secret
Each repository needs a Gemini API key to run analysis. You can use your own key for testing or a shared organization key.

1.  **Get an API Key**: [Google AI Studio](https://aistudio.google.com/app/apikey)
2.  **Add to GitHub**:
    *   Go to your Repository -> **Settings** -> **Secrets and variables** -> **Actions**.
    *   Click **New repository secret**.
    *   Name: `GEMINI_API_KEY`
    *   Value: `AIza...` (your key)

### 2. Add the Workflow File
Create a file at `.github/workflows/ai-review.yml` in your repo and paste the following:

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
```

**That’s it!** Every time you open or update a PR, the AI will automatically analyze your code and post review comments.

**That’s it!** Every time you open or update a PR, the AI will automatically analyze your code and post review comments.

---

## 💡 Why use the Action flow?

*   ✅ **Secure**: Code stays within your GitHub runner.
*   ✅ **Zero Ops**: No servers or app webhooks to maintain.
*   ✅ **Team Standard**: Uses the organization’s fine-tuned specialist agents.
*   ✅ **Flexible Billing**: Developers can use personal API keys or the shared organization key.

## ❓ Need Help?
If the review doesn't start, ensure:
1.  The `GEMINI_API_KEY` is correctly set in secrets.
2.  The workflow file is named exactly `.github/workflows/ai-review.yml`.
3.  The PR is opened against the `main` branch (or update `base_ref` in action inputs).


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

---

## Code Graph Cache

The AI Reviewer builds a **code graph** on each run to understand cross-file relationships (definitions, references, imports, data flow). This graph is saved to `.ai-reviewer/graph.json`.

### Automatic caching (built-in)

The graph is **automatically cached** across workflow runs — no setup required. The `action.yml` handles this with `actions/cache`, so on subsequent PRs the reviewer loads the cached graph instead of rebuilding it.

| Scenario | Behavior | Speed |
|:---|:---|:---|
| **First run** (no cache) | Full graph is built by parsing all files | ~300ms |
| **Subsequent runs** (cache hit) | Graph is loaded from cache | ~5ms |
| **New commits** (cache stale) | Only changed files are re-parsed (delta update) | ~50ms |
| **`--no-cache` flag** | Skips cache entirely, forces fresh analysis | ~300ms |

### Add to `.gitignore`

The graph file should not be committed:

```
# AI Reviewer cache
.ai-reviewer/
```

