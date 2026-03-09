# Multi-Repo Setup and Testing Guide

The AI Code Reviewer usually operates using the default `GITHUB_TOKEN` provided by GitHub Actions. However, this default token is strictly isolated and only has access to the repository where the workflow is currently running. 

To enable the new **Multi-Repo Context** feature (allowing the AI to analyze dependencies and references across your other repositories), you need to provide credentials that have access to multiple repositories. The most secure and robust way to do this is by creating a custom **GitHub App**.

---

## 1. Creating the GitHub App

1. Navigate to your GitHub settings:
   - For a personal account: **Settings** > **Developer settings** > **GitHub Apps**
   - For an organization: **Organization settings** > **Developer settings** > **GitHub Apps**
2. Click **New GitHub App**.
3. Fill out the basic information:
   - **GitHub App name**: e.g., `My Org AI Code Reviewer`
   - **Homepage URL**: e.g., `https://github.com` (can be anything, it's required but not used by our action)
   - **Webhook**: Uncheck the `Active` box (we don't need webhooks since we run within Actions).
4. Assign the following **Repository Permissions**:
   - **Contents**: `Read-only` *(Allows the agent to fetch code from peer repositories for context)*
   - **Pull requests**: `Read & write` *(Allows the agent to read the PR diff and post review comments)*
   - **Metadata**: `Read-only` *(Default requirement)*
5. Leave "Organization permissions" and "Account permissions" empty.
6. Under **Where can this GitHub App be installed?**, select **Any account** or **Only on this account** depending on your needs.
7. Click **Create GitHub App**.

---

## 2. Gathering the Credentials

Once the app is created, you need two pieces of information:

1. **App ID**: Found at the top of the App's "General" settings page.
2. **Private Key**: Scroll down on the General page and click **Generate a private key**. This will download a `.pem` file to your computer.
   - Open this `.pem` file in a text editor. You will need the entire block of text, including `-----BEGIN RSA PRIVATE KEY-----` and `-----END RSA PRIVATE KEY-----`.

---

## 3. Installing the App

Before the App can read your repositories, it must be installed.
1. On your App's settings page, go to **Install App** (in the left sidebar).
2. Click **Install** next to your user account or organization.
3. Select either **All repositories** or explicitly select the repositories you want the AI to cross-reference (e.g., your core libraries, frontend, and backend repos). 
4. Complete the installation.

---

## 4. Configuring GitHub Actions

You now need to pass the App credentials to the Action securely.

1. Go to the repository where you want the AI Reviewer to run.
2. Navigate to **Settings** > **Secrets and variables** > **Actions**.
3. Create two new **Repository Secrets** (or Organization Secrets if you prefer to share them across multiple repos):
   - `APP_ID`: Paste your numeric App ID here.
   - `APP_PRIVATE_KEY`: Paste the entire contents of your `.pem` file here.
4. Update your `.github/workflows/review.yml` (or whatever your workflow file is named) to pass these inputs into the action:

```yaml
jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Repo
        uses: actions/checkout@v4
        
      - name: Run AI Reviewer
        uses: ./ # Or the path/reference to your action
        with:
          github_token: ${{ secrets.GITHUB_TOKEN }}
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          app_id: ${{ secrets.APP_ID }}
          app_private_key: ${{ secrets.APP_PRIVATE_KEY }}
```

*(Note: The workflow still requires the default `github_token` as a safe fallback, but the action will prioritize the App credentials if they are provided).*

---

## 5. Testing the Multi-Repo Approach

To verify that the multi-repo approach is working:

1. Ensure the GitHub App is installed on both **Repository A** and **Repository B**.
2. Create a Pull Request in **Repository A**.
3. In this Pull Request, modify a function name, struct, or exported variable that you know is imported by **Repository B**.
4. Check the GitHub Actions logs for your PR. You should look for logs that indicate the multi-repo detection succeeded:
   - `🔑 Using GitHub App credentials...`
   - `🌐 [MultiRepo] Multi-repo review capability detected.`
   - `✅ Found cross-repo references for X peer repositories`
5. Read the final AI review comment on the Pull Request. The AI should proactively point out that you changed a symbol used by **Repository B** and advise you on the downstream impacts.
