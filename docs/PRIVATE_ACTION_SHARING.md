# Sharing Private Action & Domain Restriction

This guide covers how to share this private AI Reviewer action with your teammates and restrict execution to authors with `@appointy.com` email addresses.

## 1. Sharing a Private Action

Since this repository is private, other repositories cannot use it by default. Here are the two ways to share it:

### Option A: Organization Level (Recommended)
If your repositories are in the same GitHub Organization:
1.  Go to the **Settings** of this repository.
2.  Navigate to **Actions** -> **General**.
3.  Scroll to **Access**.
4.  Select **"Accessible from repositories in the 'Your-Org' organization"**.
    *   *Note: You can choose "Internal" or "Private repositories in this organization".*

### Option B: Using a Personal Access Token (PAT)
If you cannot change organization settings, teammates must check out the action repository manually:

```yaml
    steps:
      - uses: actions/checkout@v4
      - name: Checkout AI Reviewer Action
        uses: actions/checkout@v4
        with:
          repository: YOUR_ORG/code-review
          token: ${{ secrets.PAT_WITH_REPO_ACCESS }}
          path: .github/actions/ai-reviewer
      
      - name: Run Review
        uses: ./.github/actions/ai-reviewer
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

---

## 2. Restricting to `@appointy.com`

We have implemented an authorization check in the CLI. You can now enforce that only PRs from authorized employees are reviewed.

### How to Enable
Add the `allowed_domain` input to your workflow:

```yaml
      - name: AI Code Reviewer
        uses: YOUR_ORG/code-review@v1
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
          allowed_domain: "appointy.com" # 👈 Add this line
```

### Important Note on Privacy
GitHub users often hide their emails. For this check to be 100% reliable:
*   Users should make their `@appointy.com` email visible in their GitHub profile (Settings -> Profile -> Public email).
*   If the email is hidden, the system will **skip the review** and log: `Authorization Failed: Could not find email`.

### Alternatives
If email visibility is an issue, consider restricting **Action execution permissions** at the Organization level to only authorized teams.
