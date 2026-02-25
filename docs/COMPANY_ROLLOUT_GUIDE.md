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
    types: [opened, synchronize, reopened]

permissions:
  contents: read
  pull-requests: write

jobs:
  ai-review:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Run AI Reviewer
        uses: acme-corp/ai-code-reviewer@main # 👈 Update to your company repo
        with:
          github_token: ${{ secrets.GITHUB_TOKEN }}
          # Optional: Override the org-wide API key for this specific repo
          # gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
```

**That’s it!** The action will cache the CodeGraph automatically and run directly inside the company’s secure GitHub Runner environment.

---

## ⚠️ Alternative: Keeping it on Your Personal Account

If you **must** keep the repository Private on your personal account, the GitHub Action cannot be used natively by your company. You have two sub-optimal choices:

### Approach A: The PAT Hack (Fragile)
Every single company repository that wants to use the reviewer must check out your private code using a Personal Access Token created by you.

1. You create a Fine-Grained PAT on your account with "Read Content" access to your `ai-code-reviewer` repo.
2. The company stores your PAT as an Organization Secret (e.g., `REVIEWER_ACCESS_TOKEN`).
3. Developers use this clunky workflow:

```yaml
      - name: Checkout AI Reviewer Action
        uses: actions/checkout@v4
        with:
          repository: your-username/ai-code-reviewer
          token: ${{ secrets.REVIEWER_ACCESS_TOKEN }} 
          path: .github/actions/ai-reviewer
          ref: main

      - name: Run AI Reviewer
        uses: ./.github/actions/ai-reviewer
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
```
*Why this is bad: If you leave the company or your token expires, the entire company's CI/CD breaks instantly.*

### Approach B: Self-Hosted Webhooks (Ops Heavy)
You abandon GitHub Actions and run the reviewer as a SaaS product.
1. You deploy the Go Backend to a cloud server (AWS, Render, etc.) yourself.
2. You create a GitHub App in the company organization.
3. You set the Webhook URL of the App to point to your cloud server.
*Why this is bad: You now have to pay for server costs, maintain uptime, manage webhook retries, and scale the infrastructure.*

---

## Conclusion
For 99% of organizations, **Step 1 (Shifting the repo to the Company GitHub)** is the only maintainable, secure, and zero-ops way to deploy this tool.
