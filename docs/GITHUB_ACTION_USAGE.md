# AI Code Reviewer - GitHub Action Usage Guide

This guide explains how to integrate the localized, security-first AI Code Reviewer into your GitHub Actions workflows.

In this mode, the code **never leaves your GitHub Actions runner**. The reviewer runs entirely as a CLI tool within your CI pipeline, ensuring maximum security and compliance.

## Prerequisites

1.  **Gemini API Key**: You need a Google Gemini API Key.
    *   Get one [here](https://aistudio.google.com/app/apikey).
    *   Add it to your repository secrets: `Settings` > `Secrets and variables` > `Actions` > `New repository secret` -> Name: `GEMINI_API_KEY`.
2.  **GITHUB_TOKEN**: This is automatically provided by GitHub Actions, but you must ensure the workflow has permission to write comments.

## 1. Preparation (For You / Maintainer)

Before your team can use this, you must **Publish** the Action by creating a release on GitHub.

1.  **Push Code**: Ensure `action.yml`, `backend/`, and `go.mod` are pushed to your GitHub repository.
2.  **Create a Release**:
    *   Go to your GitHub Repository page.
    *   Click **Releases** (sidebar) -> **Draft a new release**.
    *   **Choose a tag**: Create a new tag, e.g., `v1` or `v1.0.0`.
    *   Click **Publish release**.

Now your action is addressable as: `owner/repo-name@v1`.

## 2. Usage

To enable AI code reviews, created a workflow file in your repository (e.g., `.github/workflows/review.yml`):

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
          fetch-depth: 0 # Required to access git history for diffs

      - name: AI Code Reviewer
        uses: appointytech/ai-code-reviewer@main
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

## Inputs Configuration

| Input | Required | Default | Description |
| :--- | :---: | :--- | :--- |
| `gemini_api_key` | ✅ | N/A | Your Google Gemini API Key. |
| `github_token` | ✅ | N/A | The GITHUB_TOKEN to allow posting comments on the PR. |
| `pr_number` | ❌ | Action Event | The Pull Request number. Automatically detected if running on `pull_request` event. |
| `base_ref` | ❌ | `origin/main` | The git reference to compare against. |
| `head_ref` | ❌ | `HEAD` | The git reference containing the changes (usually the current checkout). |

## Security Assurance

When running in this mode:
*   ✅ **No source code is sent to our servers.**
*   ✅ **No webhooks are used.**
*   ✅ **The only external connection is to the LLM provider (Google Gemini API).**
*   ✅ **All review logic executes locally inside the GitHub runner.**

## Troubleshooting

*   **"Error getting diff"**: Ensure `fetch-depth: 0` is set in the `actions/checkout` step. Shallow clones may not have enough history to compare branches.
*   **"Permission denied" (Commenting)**: Ensure your workflow has `permissions: pull-requests: write`.
