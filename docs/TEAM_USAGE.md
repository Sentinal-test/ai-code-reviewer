# AI Code Reviewer - Team Usage Guide

This guide details how to set up the **AI Code Reviewer** for any repository in your organization.

## 🏆 1. The Best Practice (GitHub App + Actions)
This is the industry-grade solution. It removes the need for individual API Keys and PATs.

### Why this is perfect for your team:
| Problem | Solved? |
| :--- | :--- |
| No PAT sharing | ✅ |
| Works across repos | ✅ |
| No secret rotation | ✅ |
| Least privilege | ✅ |
| Revocable instantly | ✅ |

### Teammate Workflow (Developer Experience):
Create a file at `.github/workflows/ai-review.yml` in your repository:

```yaml
name: AI Code Review
on:
  pull_request:
    types: [opened, synchronize]

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: AI Code Reviewer
        uses: tegveer-work/ai-code-reviewer@v1
```
**That’s it. No secrets. No keys. No setup.**

> [!IMPORTANT]
> This "No Setup" experience requires the **GitHub App** to be installed on your repository and the **Organization Admin** to have configured the system-wide Gemini API Key as an Organization Secret.

---

## 🔑 2. Prerequisites (For Admins)
If you are the owner of the Reviewer repo, follow the [GitHub App Setup Guide](file:///Users/tegi/Desktop/code-review/docs/github_app_setup.md) to enable this "Best Practice" flow for your team.

---

## 📦 3. Legacy Methods (PAT & Individual Keys)
Use these ONLY if you cannot use the GitHub App flow (e.g., for external collaborators).

### Option A: The "Private Repo" Method
Teammates must use a **Personal Access Token (PAT)** to "download" the action into their workflow.

```yaml
      - name: Checkout AI Reviewer Action
        uses: actions/checkout@v4
        with:
          repository: tegveer-work/ai-code-reviewer
          token: ${{ secrets.ACTION_ACCESS_TOKEN }} 
          path: .github/actions/ai-reviewer

      - name: Run AI Reviewer
        uses: ./.github/actions/ai-reviewer
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

---

## ❓ FAQ

### Which one should I use?
*   **Always use the GitHub App flow** if you are part of the internal team. It's more secure and easier to manage.
*   **Use Legacy Methods** only for trial runs or repos where you cannot install the Org-level app.

### How do I get Gemini API Keys?
If you are using the GitHub App flow, you don't need one! The system uses the centralized key. For legacy methods, get one from [Google AI Studio](https://aistudio.google.com/app/apikey).
