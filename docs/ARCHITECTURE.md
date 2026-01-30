# Architecture Overview

This document outlines the planned architecture for the AI Code Reviewer, focusing on a high-performance, internal Go-based orchestration layer.

## 🏗️ System Architecture

The core of the system is a native **Go (Golang)** application that orchestrates the entire code review lifecycle. This design eliminates external workflow dependencies to ensure low latency, strict type safety, and a simplified single-binary deployment.

### Key Components

#### 1. LLM Orchestration (`internal/llm`)
- **Purpose**: A dedicated package for interacting with Large Language Models (Gemini).
- **Responsibilities**:
    - Constructs context-aware prompts.
    - Manages direct API calls to the LLM provider.
    - Enforces strict JSON parsing to ensure structured, machine-readable output.
- **Prompt Engineering Strategy**: The system uses highly specific system prompts to enforce brevity (max 2 sentences per comment) and actionable feedback.

#### 2. GitHub Integration (`internal/github`)
- **Purpose**: A robust wrapper around the GitHub API.
- **Responsibilities**:
    - **Authentication**: Handles GitHub App JWT generation and installation token management.
    - **Workflow Management**: Fetches PR diffs, posts review comments, and manages the "Check Run" lifecycle (In Progress -> Success/Failure).
    - **Feedback Loop**: Ensures all interactions with GitHub are tracked and reported correctly.

#### 3. Shared Data Models (`internal/models`)
- **Purpose**: To provide a single source of truth for data structures.
- **Responsibilities**:
    - Defines type-safe structs (`RepoSettings`, `ReviewResult`, `ReviewComment`).
    - Ensures consistency across the GitHub and LLM packages.

## 🚀 Architectural Goals & Benefits

### 1. Low Latency ⚡
The architecture is designed to minimize network hops.
- **Flow**: Go Service -> LLM Provider -> Go Service
- **Benefit**: direct communication removes the overhead of external webhook processors, resulting in faster feedback for developers.

### 2. Simplified Infrastructure & Deployment 🧶
- **Single Source of Truth**: All business logic, prompts, and integrations live within the Go codebase.
- **Deployment**: The entire system compiles to a single Go binary (plus database), simplifying hosting, scaling, and debugging. No external workflow servers are required.

### 3. Reliability & Control 🛡️
- **Type Safety**: Go's strong typing prevents runtime errors common in loosely typed JSON workflows.
- **Resilience**: The system implements granular control over timeouts (e.g., 60s for LLM calls), retries, and failure states.
- **Security**: delicate API keys and secrets are managed internally, significantly reducing the attack surface.

### 4. Enhanced User Experience ✨
- **Real-time Feedback**: The integration allows for immediate management of GitHub Check Runs, showing "In Progress" spinners to users instantly.
- **Dynamic Configuration**: The system can dynamically adjust system prompts based on repository-specific settings (e.g., toggling "Security" or "Performance" review layers) using internal logic.

## 📂 Proposed Project Structure

The project follows a standard Go CLI/Service layout:

```
backend/
├── internal/
│   ├── database/    # DB connection & initialization
│   ├── github/      # GitHub Client & Check Run management
│   ├── llm/         # Gemini API Integration & Prompting
│   └── models/      # Shared Domain Models
├── main.go          # Application Entry point & Webhook Handler
└── ...
```
