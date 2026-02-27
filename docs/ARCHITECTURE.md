# Architecture Overview

This document outlines the architecture for the AI Code Reviewer, focusing on a high-performance, Go-based CLI orchestration layer designed to run within GitHub Actions.

## 🏗️ System Architecture

The core of the system is a native **Go (Golang)** application that orchestrates the entire code review lifecycle. This design ensures low latency, strict type safety, and a simplified single-binary execution inside the GitHub Runner.

### Key Components

#### 1. LLM Orchestration (`internal/llm`)
- **Purpose**: A dedicated package for interacting with Large Language Models (Gemini 2.5 Pro & Flash).
- **Responsibilities**:
    - Constructs context-aware prompts with the Code Graph.
    - Manages agentic loops: Model → Tools → Model.
    - Handles semantic consolidation of multiple specialist agent results.

#### 2. GitHub Integration (`internal/github`)
- **Purpose**: A robust wrapper around the GitHub API for CI environments.
- **Responsibilities**:
    - **Authentication**: Uses the local `GITHUB_TOKEN` provided by the Action runner.
    - **Workflow Management**: Fetches PR diffs and metadata.
    - **Commenting**: Posts intelligent inline comments and general PR summaries.

#### 3. Agentic Code Graph (`internal/codegraph`)
- **Purpose**: Provides deep, deterministic codebase context.
- **Responsibilities**:
    - Parses AST to resolve imports and symbol definitions.
    - Exposes tools to the LLM (e.g., `get_callers`, `get_symbol_definition`).
    - Maintains a local cache for ultra-fast incremental analysis.

## 🚀 Architectural Goals & Benefits

### 1. Security-First (Zero Ops) ⚡
The architecture is designed to run entirely within the user's GitHub Actions environment.
- **Flow**: Runner -> Local Binary -> Google Gemini -> Post Comments.
- **Benefit**: Code stays within your security perimeter; no external webhook servers or databases are required for basic reviews.

### 2. Specialist Parallelism 🏗️
- **Execution**: The orchestrator launches three specialized reasoning agents (Correctness, Security, Structure) in parallel goroutines.
- **Benefit**: Massive reduction in wall-clock time and significant improvement in audit depth.

### 3. Reliability & Control 🛡️
- **Type Safety**: Go's strong typing prevents runtime errors common in loosely typed LLM workflows.
- **Deterministic Context**: The Code Graph ensures agents see exactly what they need, preventing the "hallucinations" common when LLMs are given limited diff hunks.

## 📂 Project Structure

```
backend/
├── cmd/
│   └── cli/         # Entry point (GitHub Action binary)
├── internal/
│   ├── agents/      # Specialist persona logic
│   ├── codegraph/   # AST & dependency resolution
│   ├── github/      # GitHub API integration
│   ├── llm/         # Gemini client & consolidator
│   └── orchestrator/# Parallel fan-out coordinator
└── main.go          # CLI entry point logic
```
