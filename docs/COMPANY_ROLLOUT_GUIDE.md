# 🚀 Company Rollout Guide: Deploying AI Code Reviewer

This guide is for developers looking to deploy the AI Code Reviewer across their entire organization/company.

## ✨ The Golden Rule: Shift to Company GitHub (Recommended)

If your current `ai-code-reviewer` repository is **Private** on your **personal account**, you will face severe friction trying to use it in your company's repositories. Cross-organization private action sharing requires messy Personal Access Tokens (PATs).

**THE BEST WAY**: Transfer or duplicate the `ai-code-reviewer` repository into your **Company's GitHub Organization** as an **Internal** or **Private** repository.

Here is exactly how to do it:

---

### Step 1: Move to Company GitHub
1. Create a new Private or Internal repository in your company’s GitHub organization (e.g., `acme-corp/ai-code-reviewer`).
2. Push the code from your personal account to this new company repository.

### Step 2: Enable Organization-Wide Access
GitHub has a built-in feature to share actions securely across an organization without making the code public.

1. Go to your new company repo: `acme-corp/ai-code-reviewer`
2. Navigate to **Settings > Actions > General**.
3. Scroll down to **Access**.
4. Select **"Accessible from repositories in the '[Your-Company]' organization"**.
5. Click Save.

### Step 3: Set Up the API Key in the Company Org
The AI Reviewer needs a Gemini API key to function. You have two options for managing this:

**Option A: Organization-Wide Default (Recommended)**
Instead of adding a key to every single repository, set it once centrally.
1. Go to your **`acme-corp/ai-code-reviewer`** repository (the one you just created).
2. Go to **Settings > Secrets and variables > Actions**.
3. Add a new repository secret named `ORG_DEFAULT_GEMINI_KEY`.
4. Now, any repository in your organization that uses the action will automatically use this key.

**Option B: Per-Repository Override**
If specific repositories need to use their own billing/keys, they can add `GEMINI_API_KEY` to their own repository secrets and pass it explicitly in the workflow (see Step 4). This overrides the organization default.

### Step 4: The Developer Workflow (One File Setup)
Now, any team within your company can enable AI code reviews by simply dropping a single YAML file into their repository. No servers to manage, no webhooks, zero ops.

Have developers add this file to `.github/workflows/ai-review.yml` in their projects:

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

**That’s it!** The action will cache the CodeGraph automatically and run directly inside the company’s secure GitHub Runner environment.

---

## 🛠️ Security & Privacy

By using the GitHub Action flow:
1.  **Code stays in CI**: Your source code is checked out on the GitHub Runner and never sent to a third-party server.
2.  **LLM Only**: Only the relevant diff and dependency snippets are sent to Google Gemini via encrypted API calls.
3.  **No Persistent Access**: The `GITHUB_TOKEN` is short-lived and scoped only to the current Pull Request.

---

## Conclusion

The GitHub Action-only architecture is the most maintainable, secure, and zero-ops way to deploy this tool across a large organization. By setting organization-wide secrets, you can enable industry-grade reviews for hundreds of repositories in minutes.
