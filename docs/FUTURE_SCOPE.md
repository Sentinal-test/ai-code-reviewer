# Future Scope & Roadmap

This document outlines the strategic vision and upcoming improvements for the AI Code Reviewer. It focuses on optimization, user experience enhancements, and broader ecosystem integration.

## 🚀 Performance & Cost Optimization

### 1. Single LLM Call per Review Layer
- **Current State**: The system often makes multiple calls or separates Context gathering ("Scout") from Review.
- **Future Impl**: Optimize the prompt engineering to handle multiple review layers (e.g., Security + Performance + Bugs) in a **single inference pass**.
- **Benefit**: Drastically reduces latency and API costs while maintaining high-quality feedback.

### 2. Intelligent Caching Strategy
- **Idea**: Hash file contents and dependency graphs to avoid re-reviewing code that hasn't changed between commits or re-runs.
- **Benefit**: Saves tokens and provides near-instant results for unmodified sections of a PR.

## 🖥️ Dashboard Evolution (Next.js)

### 3. Dashboard 2.0 (Beyond MVP)
- **Current State**: A basic interface for settings and toggles.
- **Future Impl**:
    - **Visual Review History**: See a timeline of all reviews, passed/failed checks, and money saved.
    - **Interactive Config**: A drag-and-drop editor to build custom review pipelines.
    - **User Management**: Role-based access control (RBAC) for teams (Admin, Developer, Viewer).

### 4. Advanced Analytics & Insights
- **Idea**: Visualize trends in code quality over time.
- **Metrics**:
    - Most common types of bugs found (e.g., "70% of issues were NULL pointer dereferences").
    - Top "Offenders" files (files that break most often).
    - Review helpfulness rating (Thumbs up/down from developers).

## 🧩 Ecosystem & Extensions

### 5. Custom Rule Engine
- **Idea**: Allow teams to define custom rules in natural language or Regex.
- **Example**: "Ensure all database calls use the `ctx` context" or "Forbid usage of `fmt.Println` in production code."

### 6. Multi-LLM Provider Support
- **Idea**: Decouple the `internal/llm` package to support other providers (OpenAI, Anthropic) or local models (Llama 3, Mistral) for privacy-conscious teams.

### 7. IDE Extension
- **Idea**: Bring the reviewer closer to the developer.
- **Impl**: A VS Code extension that runs a "Pre-flight" check locally before the code is even pushed to GitHub.

## 🛠️ DevOps & Deployment

### 8. Self-Hosted Enterprise Docker Image
- **Idea**: Package the Go binary, Postgres, and a local LLM gateway into a single `docker-compose` file.
- **Benefit**: strict data compliance for enterprise clients who cannot leak code to public cloud LLMs.
