# Enterprise Deployment Plan: AI Code Reviewer (Sentinel)

This document outlines the final plan for deploying the AI Code Reviewer within your company while keeping the source code **Private**.

## 1. The Strategy: Hybrid Power
We combine the **GitHub App** for easy installation with a **Private GitHub Action** for controlled execution.

---

## 2. Option A: SaaS Mode (Zero-Config)
**Best for**: Speed and simplicity across all company repos.
*   **How it works**: Developers simply install the GitHub App on their repository.
*   **No YAML required**: The backend server automatically detects Pull Requests and posts comments.
*   **Security**: Since the code is reviewed by your self-hosted Go backend, the source code of the reviewer stays entirely private on your server.

**Setup**:
1.  Go to your **GitHub App Settings**.
2.  Ensure **Webhooks** are active and pointing to your Go Backend URL.
3.  Developers install the App from: `https://github.com/settings/apps/[your-app-name]/installations`.

---

## 3. Option B: Private Action (Company-Wide)
**Best for**: Teams that want to run the reviewer as part of their CI/CD and keep the code inside the GitHub Runner.
*   **Constraint**: Because the repo is private, you must grant access to other repositories.

### Step 1: Grant Organization Access
On the `ai-code-reviewer` repository:
1.  Go to **Settings > Actions > General**.
2.  Scroll to **Access**.
3.  Select: **"Accessible from repositories in the '[Your-Org]' organization"**.
4.  *Optional*: Select "All repositories" if you want every repo in the org to use it.

### Step 2: Developer Workflow
Developers add this to their `.github/workflows/ai-review.yml`:

```yaml
      - name: Run AI Reviewer
        uses: [Your-Org]/ai-code-reviewer@main
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

---

## 4. Option C: Cross-Account Access (External Developers)
If you have developers outside your organization (different GitHub ID):
*   **Method**: Use a Personal Access Token (PAT) to checkout the action manually.
*   **Workflow**:
```yaml
      - name: Checkout AI Reviewer Action
        uses: actions/checkout@v4
        with:
          repository: [Your-Org]/ai-code-reviewer
          token: ${{ secrets.ACTION_ACCESS_TOKEN }} 
          path: .github/actions/ai-reviewer

      - name: Run AI Reviewer
        uses: ./.github/actions/ai-reviewer
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

---

## 5. Security & Secrets Management
1.  **`GEMINI_API_KEY`**: Every repository using the Action MUST have this in their **Action Secrets**.
2.  **GitHub App Installation**: This is required for Step 1 (Organization Access) to work seamlessly if you are using App-level permissions.

## 6. Checklist for Success
- [ ] Go Backend is running and reachable via Webhook (for SaaS mode).
- [ ] `action.yml` is updated with the latest resilient parsing logic.
- [ ] Repo Settings -> Actions -> General -> Access is set to "Organization Wide".
- [ ] Developers have the `GEMINI_API_KEY` added to their repository.
