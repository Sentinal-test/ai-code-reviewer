# GitHub App Setup Guide (Internal Org Reviewer)

This guide covers the end-to-end setup of a Private GitHub App to power the AI Code Reviewer. This configuration is optimized for **private organizations** where the code review runs entirely on **GitHub Actions runners**, ensuring code never leaves the GitHub environment.

---

## 1. Create the GitHub App

1.  Navigate to your **Organization Settings**.
2.  In the left sidebar, click **Developer settings** > **GitHub Apps**.
3.  Click **New GitHub App**.
4.  **App Name**: e.g., `Org-AI-Reviewer` (Must be unique across GitHub).
5.  **Homepage URL**: Use your Org's main URL or your reviewer repo URL.
6.  **Webhook**: 
    - Uncheck **Active** (Since we trigger via GitHub Actions `on: pull_request`, we don't need a persistent webhook server).
    - *Note: If you plan to use this app later to trigger reviews outside of Actions, you can always enable this.*

---

## 2. Repository Permissions

To maintain high security, we follow the **Principle of Least Privilege**. Enable only the following:

| Permission | Access | Why? |
| :--- | :--- | :--- |
| **Pull requests** | **Read & Write** | **MANDATORY.** To read diffs and post review comments. |
| **Contents** | **Read-only** | To read the source code files for deep context. |
| **Checks** | **Read & Write** | To create "Check Runs" so developers see the status in the PR UI. |
| **Commit statuses** | **Read & Write** | To mark the PR as passed/failed based on critical findings. |
| **Metadata** | **Read-only** | Mandatory for all apps to function. |
| **Issues** | **Read & Write** | Optional. Only if you want to post summaries in the issue thread. |

> [!WARNING]
> **Administration**: Set to **No Access**. The reviewer never needs to modify repository settings or collaborators.
> **Workflows**: Set to **No Access**. The reviewer should not be able to modify your CI/CD files.

---

## 3. Organization Permissions

Set all to **No Access**. The reviewer operates at the repository level and doesn't need to manage organization members or settings.

---

## 4. Subscribe to Events

Since we are running via GitHub Actions, the events are handled by the workflow trigger. However, for future-proofing and metadata access, subscribe to:
- **Pull request**
- **Pull request review**
- **Pull request review comment**

---

## 5. Security & Visibility

1.  **Where can this GitHub App be installed?**
    - Select **Only on this account** (Private). This ensures only your organization can use the app.
2.  Click **Create GitHub App**.

---

## 6. Generate Credentials

Once the app is created, you need to collect four pieces of information for your GitHub Actions secrets:

1.  **App ID**: Found in the "General" tab (e.g., `2840769`).
2.  **Client ID**: Found in the "General" tab.
3.  **Client Secret**: Click **Generate a new client secret**. Copy it immediately.
4.  **Private Key**: Scroll down to the "Private keys" section and click **Generate a private key**. A `.pem` file will download.

---

## 7. Configuration in Organizations/Repositories

### A. Install the App
1.  In your App settings, click **Install App** in the sidebar.
2.  Click **Install** next to your Organization.
3.  Select **All repositories** or **Only select repositories**.

### B. Set Secrets
Go to your **Organization Settings** > **Actions** > **Secrets and variables** > **Actions** and add:

- `AI_REVIEWER_APP_ID`: Your App ID.
- `AI_REVIEWER_PRIVATE_KEY`: Copy the entire content of the `.pem` file (including `-----BEGIN RSA PRIVATE KEY-----`).
- `GEMINI_API_KEY`: Your Gemini API key.

---

## 8. GitHub Actions Workflow Example

Place this in `.github/workflows/ai-review.yml` in your target repositories:

```yaml
name: AI Code Review

on:
  pull_request:
    types: [opened, synchronized]

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: AI Reviewer
        uses: your-org/ai-reviewer-action@v1
        with:
          github_app_id: ${{ secrets.AI_REVIEWER_APP_ID }}
          github_private_key: ${{ secrets.AI_REVIEWER_PRIVATE_KEY }}
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
```
