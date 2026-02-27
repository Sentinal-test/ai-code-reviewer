# Project Overview: AI Code Reviewer

## What is this?
The **AI Code Reviewer** is an automated defect detection system designed to provide hyper-critical, security-focused code reviews directly on GitHub Pull Requests. Unlike generic AI review tools, this system is built to behave like a senior security and reliability auditor, focusing on identifying bugs, vulnerabilities, and architectural inconsistencies rather than providing conversational praise.

## Key Features
- **Parallel Specialist Agents**: Instead of one generic prompt, the system runs 3 distinct agents in parallel per file chunk:
    - **🐛 Correctness Agent**: Hunts for logic bugs and state leaks.
    - **🔐 Security Agent**: Traces attack paths and auth bypasses.
    - **🏗️ Structure Agent**: Acts as an architect, ensuring consistency across files.
- **Intelligent Consolidation**: A final LLM pass that semantically merges, deduplicates, and formats the findings of the 3 specialists into a single clean review.
- **Agentic Code Graph Tools**: Agents aren't just reading static text. They are equipped with tools (`get_callers`, `get_symbol_definition`, `search_codebase`) to actively traverse the codebase, verifying assumptions and preventing false positives before leaving a comment.
- **Deep Contextual Awareness**:
    - **Slim Dependency injection**: Instead of stuffing full files into context, agents receive a slim index of imported signatures.
    - **Diff-Only Mode**: Prevents context overflow on massive, generated, or lock files.
    - **Repo Structure**: Injects the directory tree so architectural decisions make sense.
- **Automated Defect Detection**: Strictly identifies logic errors, security risks (injection, auth bypass), performance bottlenecks, and resource leaks.
- **Hybrid Commenting**: Automatically posts inline comments on the diff when possible, falling back to general PR comments if a critical issue is found in a related (unchanged) file.
- **Configurable Control**: A Next.js dashboard allows users to manage repository settings, toggle review layers (Security, Bug, Lint, etc.), and provide custom LLM API keys.

## Why we built this
Manual code reviews are time-consuming and often miss subtle logic or security flaws. This tool aims to act as a "first pass" safety net that catches high-severity issues before a human reviewer even opens the PR, allowing developers to iterate faster and maintain a higher bar for code quality.
