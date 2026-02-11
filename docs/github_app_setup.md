# GitHub App Creation Guide for AI Code Reviewer

This guide outlines the steps to set up a GitHub App for the AI Code Reviewer using the industry-grade "GitHub App + GitHub Actions" method. This approach ensures security, scalability, and ease of use for teams.

## Why this is the best practice?

| Problem | Solved? |
| :--- | :--- |
| No PAT sharing | ✅ |
| Works across repos | ✅ |
| No secret rotation | ✅ |
| Least privilege | ✅ |
| Revocable instantly | ✅ |
| Enterprise-grade | ✅ |

## Step-by-Step Creation Guide

### 1. Create the GitHub App

1.  Go to your GitHub Organization or Personal account settings.
2.  Navigate to **Developer settings** > **GitHub Apps** > **New GitHub App**.
3.  **Name**: Choose a unique name (e.g., `ai-code-reviewer-bot`).
4.  **Homepage URL**: You can use your repository URL (e.g., `https://github.com/your-org/ai-code-reviewer`).
5.  **Callback URL**: `http://localhost:3000/api/auth/callback/github` (or your production dashboard URL).
6.  **Webhook**: Active.
    *   **Webhook URL**: Your backend webhook endpoint (e.g., `https://your-api.com/webhook`).
7.  **Permissions**:
    *   **Repository permissions**:
        *   `Pull Requests`: **Read** (to read PR diffs)
        *   `Contents`: **Read** (to read code files)
        *   `Checks`: **Write** (to post status checks)
    *   **Subscribe to events**:
        *   Select `Pull request`

8.  Click **Create GitHub App**.

### 2. Retrieve Credentials

Once created, you will see the App's specific information:

1.  **App ID**: Note this down (e.g., `123456`).
2.  **Private Key**: Scroll down to generate a private key. This will download a `.pem` file. **Keep this secure.**

### 3. Store Credentials in Your Repository

You (the owner of the `ai-code-reviewer` action repo) need to store these secrets so your Action can use them to generate tokens.

1.  Go to your repository's **Settings** > **Secrets and variables** > **Actions**.
2.  Add a **New repository secret**:
    *   Name: `APP_ID`
    *   Value: The App ID you noted earlier.
3.  Add another **New repository secret**:
    *   Name: `APP_PRIVATE_KEY`
    *   Value: The content of the `.pem` file you downloaded.

### 4. Install the App
The App MUST be installed on the repositories where you want it to run.
1.  On the App settings page, click **Install App** in the sidebar.
2.  Click **Install** next to your organization/account.
3.  Select **All repositories** or **Only select repositories** (ensure the Reviewer repo and target repos are included).

### 5. Usage in GitHub Actions

In your workflow file (e.g., `.github/workflows/review.yml`), you will use these secrets to generate a short-lived token.

```yaml
steps:
  - name: Generate token
    id: generate_token
    uses: actions/create-github-app-token@v1
    with:
      app-id: ${{ secrets.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}

  - name: Run AI Reviewer
    uses: your-org/ai-code-reviewer@v1
    env:
      GITHUB_TOKEN: ${{ steps.generate_token.outputs.token }}
```

## Security Model

`Repo → GitHub Actions → GitHub App token → GitHub API`

*   **No human credentials anywhere.**
*   The token lives for ~1 hour.
*   The token is scoped only to that repository.
*   It cannot be reused elsewhere.
