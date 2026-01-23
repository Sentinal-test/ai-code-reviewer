# Architecture Migration: Removing n8n for Internal Orchestration

This document outlines the architectural shift from using n8n for AI orchestration to a fully internal Go-based implementation.

## 🏗️ What We Built
We replaced the external **n8n** workflow dependency with a native **Go (Golang)** internal orchestration layer.

### Key Components
1.  **`internal/llm`**:
    - A dedicated package for interacting with Large Language Models (Gemini).
    - Handles prompt construction, API calls, and strict JSON parsing.
    - **Prompt Engineering**: Enforces brevity (max 2 sentences), actionable feedback, and structured JSON output.

2.  **`internal/github`**:
    - A clean wrapper around the GitHub API.
    - Handles authentication (GitHub App JWTs), diff fetching, comment posting, and **Check Run** management.
    - Manages the lifecycle of a review (In Progress -> Success/Failure).

3.  **`internal/models`**:
    - Shared, type-safe structs (`RepoSettings`, `ReviewResult`, `ReviewComment`) used across the application to ensure data consistency.

## 🚀 Why We Removed n8n (Benefits)

### 1. Reduced Latency ⚡
- **Old**: Go -> n8n Webhook -> HTTP Request -> LLM -> n8n Logic -> Response -> Go.
- **New**: Go -> LLM -> Go.
- **Result**: Removed multiple network hops and external processing overhead, resulting in faster feedback loops.

### 2. Simplified Architecture 🧶
- **Dependency Removal**: No need to host, manage, or debug an external n8n instance.
- **Single Source of Truth**: All logic (business rules, prompts, GitHub interactions) lives in the Go codebase.
- **Deployment**: Easier to deploy (just a single Go binary + DB) without coordinating with a separate workflow automation server.

### 3. Better Control & Reliability 🛡️
- **Type Safety**: Go's strong typing prevents runtime errors that might occur in loosely typed JSON workflows.
- **Error Handling**: Granular control over timeouts (60s), retries, and failure states (e.g., updating GitHub Check Runs on failure).
- **Security**: API keys and secrets are managed internally in the backend's environment/database, shrinking the attack surface.

### 4. Improved UX ✨
- **Check Runs**: We can now easily manage GitHub Check Runs ("In Progress" spinners) directly from the code before and after the LLM call.
- **Dynamic Prompts**: Easier to dynamically adjust the system prompt based on repository settings (e.g., enabling/disabling specific review layers like Security or Performance) using Go logic.

## 📂 New Folder Structure

```
backend/
├── internal/
│   ├── database/    # DB connection & initialization
│   ├── github/      # GitHub Client & Check Runs
│   ├── llm/         # Gemini API Integration
│   └── models/      # Shared Structs
├── main.go          # Entry point & Webhook Handler
├── db.go            # (Deprecated/Moved)
├── n8n.go           # (Deleted)
└── ...
```
