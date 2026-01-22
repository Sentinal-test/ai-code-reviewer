# Layered AI Code Review System - Staff Engineer Handover

This repository contains a hardened, production-ready MVP for an automated, layered AI code review system using a Go backend, Next.js dashboard, and n8n orchestration.

## 🚀 Quick Start (Local Development)

### 1. Backend Setup (Go)
1.  **Dependencies**: Run `go mod tidy` in the `backend/` directory.
2.  **Environment**: Ensure your `backend/.env` file contains:
    ```env
    GITHUB_APP_ID="your_app_id"
    GITHUB_PRIVATE_KEY_PATH="./private-key.pem"
    GITHUB_WEBHOOK_SECRET="your_shared_secret"
    N8N_WEBHOOK_URL="http://your-n8n-instance/webhook/review"
    ```
3.  **Run**: `go run .`
    - The server starts on port `8080`.
    - It validates all environment variables at startup.

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
- [ ] If signature validation fails, you will see a red "❌ Signature Validation Failed" log.

### ✅ n8n Integration
- [ ] n8n should receive a POST request with the PR diff and repo settings.
- [ ] Check n8n's "Executions" tab to see the Gemini review in progress.
- [ ] Ensure `N8N_WEBHOOK_URL` in `.env` is the **Production URL** (not `-test`).

### ✅ GitHub Comments
- [ ] Inline comments appear on the "Files changed" tab of your Pull Request.
- [ ] Logs in Go should show "💬 Posting X comments...".

---

## 🛡️ Hardening Features Included
- **Background Processing**: PR reviews run in background goroutines to prevent webhook timeouts.
- **Robust Signature Auth**: Cryptographic validation of every incoming GitHub payload.
- **n8n Timeouts**: 30-second timeout on n8n calls to prevent the Go server from hanging.
- **Detailed Traceability**: Every event is logged with its GitHub Delivery ID for easy debugging in the GitHub App Advanced tab.
