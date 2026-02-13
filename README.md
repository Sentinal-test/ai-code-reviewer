# 🧪 Layered AI Code Review System

An automated, hyper-critical code defect detection system. It leverages a Go backend, a multi-pass LLM orchestration strategy (Gemini), and a Next.js dashboard to provide deep, contextual feedback directly on GitHub Pull Requests.

## 🔄 Application Flow

```mermaid
flowchart TD
    A[Pull Request Event] -->|GitHub Webhook| B[Go Backend]

    B --> C{Scout Pass}

    subgraph Scout Pass
        C1[Analyze Diff]
        C2[Parse File Tree]
    end

    C --> C1
    C --> C2

    C1 --> D[Identify Dependencies]
    C2 --> D

    D --> E[Fetch Files from GitHub]

    E --> F{Reviewer Pass}

    subgraph Reviewer Pass
        F1[Context Analysis]
        F2[Diff Review]
    end

    F --> F1
    F --> F2

    F1 --> G[Detect Defects]
    F2 --> G

    G --> H[Post Comments]

    H --> I[Inline Comment - Valid Line]
    H --> J[General Comment - Context Line]

```


---

## 🚀 Team Developer Quick-Start

Want AI code reviews on your repo? It takes less than a minute.

1.  **[Install the GitHub App](https://github.com/settings/apps/sentinal-review/installations)** on your repository.
2.  **Add `GEMINI_API_KEY`** to your repository secrets (Settings > Secrets > Actions).
3.  Add a simple workflow file at `.github/workflows/ai-review.yml`:

```yaml
name: AI Code Review
on:
  pull_request:
    types: [opened, synchronize]

permissions:
  contents: read
  pull-requests: write

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Run AI Reviewer
        uses: tegveer-work/ai-code-reviewer@main 
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}

```

👉 **[See the Full Developer Guide](./docs/DEVELOPER_GUIDE.md)**

---

## 🛠️ How it Works

1.  **Intent Discovery**: The system reads your PR title, description, and last 10 commits to understand *what* you are trying to build.
2.  **Breadcrumb Strategy (Scout)**: An LLM analyzes the file structure and the changed code to find "missing pieces" (e.g., if you changed a model call, it asks to read the model definition).
3.  **Audit Mode**: The Reviewer LLM is placed in a "Security & Reliability Audit" mode. It is forbidden from praising your code and focuses entirely on finding bugs, security flaws, and performance leaks using the **Full File Context**.
4.  **Resilient Feedback**: If the AI finds a bug in a related file (not the one you edited), it will still tell you via a general PR comment.

---

## 🚀 Local Development Setup

### 1. GitHub App Setup
1.  Go to **GitHub Settings > Developer Settings > GitHub Apps > New GitHub App**.
2.  Set **Webhook URL** to your ngrok URL (e.g., `https://xyz.ngrok-free.app/webhook`).
3.  Set a **Webhook Secret** (any random string).
4.  **Permissions**:
    - `Pull Requests`: Read & Write
    - `Checks`: Read & Write
    - `Contents`: Read (for fetching files)
    - `Metadata`: Read
5.  **Events**: Subscribe to `Pull request`.
6.  Generate a **Private Key** (.pem) and save it as `backend/private-key.pem`.
7.  Note your **App ID**, **Client ID**, and **Client Secret**.

### 2. Backend Setup (Go)
1.  Navigate to `backend/`: `cd backend`
2.  Install dependencies: `go mod tidy`
3.  Create `.env` file:
    ```env
    # GitHub App Credentials
    GITHUB_APP_ID="123456"
    GITHUB_CLIENT_ID="your_client_id"
    GITHUB_CLIENT_SECRET="your_client_secret"
    GITHUB_PRIVATE_KEY_PATH="./private-key.pem"
    GITHUB_WEBHOOK_SECRET="your_shared_secret"

    # LLM API Key
    GEMINI_API_KEY="your_google_ai_studio_api_key"

    # Server Config
    PORT="8080"
    ```
4.  Run the server: `go run main.go`

### 3. Frontend Setup (Next.js)
1.  Navigate to `dashboard/`: `cd dashboard`
2.  Install dependencies: `npm install`
3.  Run development server: `npm run dev`
4.  Access at `http://localhost:3000`. Use the dashboard to enable/disable specific review layers (Security, Performance, etc.) for each repo.

### 4. Running Checks
1.  Expose your local port: `ngrok http 8080`
2.  Update the **Webhook URL** in your GitHub App settings to match the ngrok URL.
3.  Open a Pull Request on a repository where you've installed the app.
4.  Watch the backend logs and your PR "Checks" tab!

---

---

## 🤖 GitHub Actions Integration

You can run the AI Code Reviewer entirely within your repository using GitHub Actions. This is the **Security-First** mode where your code never leaves the GitHub runner except for LLM API calls.

### 🔑 1. Configure Secrets
Add the following secrets to your repository (**Settings > Secrets and variables > Actions**):

### 📦 2. Choose Your Setup

> [!IMPORTANT]
> The simple setup (Option B) ONLY works if your AI Reviewer repository is **Public**. If it is **Private**, you MUST use Option A.

#### Option A: Private Action (Recommended for Teams)

Use this if you want to keep the AI Reviewer code private.

Create `.github/workflows/ai-review.yml`:
```yaml
name: AI Code Review
on:
  pull_request:
    types: [opened, synchronize]

jobs:
  review:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write # Required to post comments
    steps:
      - name: Checkout Your Code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Checkout AI Reviewer Action
        uses: actions/checkout@v4
        with:
          repository: tegveer-work/ai-code-reviewer # Update to your repo name
          token: ${{ secrets.ACTION_ACCESS_TOKEN }} 
          path: .github/actions/ai-reviewer
          ref: main

      - name: Run AI Reviewer
        uses: ./.github/actions/ai-reviewer
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

#### Option B: Public Action (Easiest)
If you make the AI Reviewer repository **Public**, anyone can use it with a single line:

```yaml
      - name: AI Code Reviewer
        uses: tegveer-work/ai-code-reviewer@main 
        with:
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

---

## 🛡️ Key Documentation
- [Team Usage Guide](./docs/TEAM_USAGE.md) - Deep dive for teammates.
- [Project Overview](./docs/PROJECT_OVERVIEW.md)
- [Architectural Decision Records](./docs/ARCHITECTURAL_DECISION_RECORDS.md)
- [Context Window Management](./docs/CONTEXT_WINDOW_MANAGEMENT.md)
- [Accuracy Improvement Plan](./docs/ACCURACY_IMPROVEMENT_PLAN.md)
- [**Troubleshooting Guide**](./docs/DEVELOPER_GUIDE.md#troubleshooting)


