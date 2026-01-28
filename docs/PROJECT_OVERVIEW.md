# Project Overview: AI Code Reviewer

## What is this?
The **AI Code Reviewer** is an automated defect detection system designed to provide hyper-critical, security-focused code reviews directly on GitHub Pull Requests. Unlike generic AI review tools, this system is built to behave like a senior security and reliability auditor, focusing on identifying bugs, vulnerabilities, and architectural inconsistencies rather than providing conversational praise.

## Key Features
- **Multi-Pass LLM Orchestration**: Uses a two-step "Scout & Review" process to ensure the model has all necessary context before making a evaluation.
- **Deep Contextual Awareness**:
    - **Full File Content**: Reviews the entire changed file, not just the isolated diff lines.
    - **Dependency Resolution**: Automatically identifies and fetches related files (types, interfaces, shared state) to verify logic across boundaries.
    - **Repo Structure**: understands the overall project layout for architectural consistency checks.
    - **PR History**: Incorporates the PR title, description, and the last 10 commit messages to understand developer intent.
- **Automated Defect Detection**: Strictly identifies logic errors, security risks (injection, auth bypass), performance bottlenecks, and resource leaks.
- **Hybrid Commenting**: Automatically posts inline comments on the diff when possible, falling back to general PR comments if a critical issue is found in a related (unchanged) file.
- **Configurable Control**: A Next.js dashboard allows users to manage repository settings, toggle review layers (Security, Bug, Lint, etc.), and provide custom LLM API keys.

## Why we built this
Manual code reviews are time-consuming and often miss subtle logic or security flaws. This tool aims to act as a "first pass" safety net that catches high-severity issues before a human reviewer even opens the PR, allowing developers to iterate faster and maintain a higher bar for code quality.
