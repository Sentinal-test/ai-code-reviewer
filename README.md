# Layered AI Code Review System

This repository contains a high-performance, automated AI code review system. It uses a **Go backend** with **internal LLM orchestration** (Gemini) to review Pull Requests in real-time, coupled with a **Next.js dashboard** for configuration.

## 🚀 Quick Start (Local Development)

### 1. Backend Setup (Go)
1.  **Dependencies**: Run `go mod tidy` in the `backend/` directory.
2.  **Environment**: Create `backend/.env` with:
    ```env
    # GitHub App Configuration
    GITHUB_APP_ID="your_app_id"
    GITHUB_PRIVATE_KEY_PATH="./private-key.pem"
    GITHUB_WEBHOOK_SECRET="your_shared_secret"
    
    # LLM Configuration
    GEMINI_API_KEY="your_gemini_api_key"
    ```
3.  **Run**: `go run .`
    - Server starts on port `8080`.
    - Validates environment variables and DB connection at startup.

### 2. Frontend Setup (Next.js)
1.  **Dependencies**: Run `npm install` in the `dashboard/` directory.
2.  **Run**: `npm run dev`
    - Dashboard available at `http://localhost:3000`.

### 3. Exposing for GitHub (ngrok)
Run `ngrok http 8080` and update your GitHub App's **Webhook URL** to `https://<your-ngrok-id>.ngrok-free.app/webhook`.

---

## 🛠️ Verification Checklist

### ✅ Webhook Connectivity
- [ ] Receive a "Received Webhook" log in the Go terminal when you push/open a PR.
- [ ] Log should show "🐙 Pull Request Event | Action: opened".

### ✅ AI Review Process
- [ ] **GitHub Check Run**: A check named "AI Code Review" should appear on the PR as "In Progress".
- [ ] **LLM Processing**: Logs should show "🧠 Running LLM review...".
- [ ] **Completion**: Check Run updates to "Success" and comments are posted.

### ✅ GitHub Comments
- [ ] Inline comments appear on the "Files changed" tab.
- [ ] Comments are prefixed with emojis (e.g., 🔐 Security, 🐛 Bug).
- [ ] Logs show "💬 Posting X comments...".

---

## 🛡️ Architecture Highlights
- **Internal Orchestration**: Direct integration with Gemini API (no external low-code tools).
- **Type-Safe**: Fully typed Go implementation for GitHub interactions and LLM responses.
- **Resilient**: Background processing, comprehensive error handling, and timeout management (60s).
- **Secure**: Robust HMAC signature validation for all webhooks.
