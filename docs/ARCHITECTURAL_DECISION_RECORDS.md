# Architectural Decision Records (ADR)

This document tracks major architectural decisions made during the development of the AI Code Reviewer.

## ADR 1: Migration from n8n to Custom Go Backend
**Context**:  
Initially, the review logic was implemented using n8n (a low-code workflow tool). 

**Decision**:  
Rewrite the core logic in Go as a standalone backend service.

**Rationale**:  
- **Reliability**: Custom Go code provides better control over error handling, retries, and background processing compared to visual workflows.
- **Maintainability**: Low-code workflows are difficult to version control, test, and refactor.
- **Performance**: Direct integration with the Gemini and GitHub APIs avoids the overhead and latency of a middle-man orchestration layer.
- **Complexity**: As the review logic became more sophisticated (requiring complex dependency tracing), n8n became a bottleneck.

---

## ADR 2: Deterministic Code Graph Context (Replacing LLM Scout)
**Context**:  
Single-pass reviews often miss context. The LLM only sees the diff and might guess incorrectly about used types or function signatures defined in other files. Initially, an LLM "Scout" was used to predict which files were needed, but this was slow, non-deterministic, and prone to hallucination.

**Decision**:  
Implement a `CodeGraph` engine using tree-sitter.
1.  **AST Parsing**: Parse all changed files to extract precise symbols (function calls, type references) using tree-sitter grammar.
2.  **Definition Resolution**: Scan the repository to find exactly where those symbols are defined.
3.  **Context Assembly**: Extract small snippets of the actual definitions (max 500 chars) and build a deterministic context graph indicating caller-callee relationships and data flow.

**Rationale**:  
- **Accuracy**: Eliminates LLM hallucinations. Context is guaranteed to be real code.
- **Speed**: Processing ASTs locally in Go takes milliseconds compared to LLM latency which took tens of seconds.
- **Context Economy**: Precisely extracts only the definition snippets (not full files), conserving the LLM context window for actual analysis.

---

## ADR 3: Full Context Enrichment (Beyond the Diff)
**Context**:  
Isolated diff lines are often insufficient for understanding complex logic or catching bugs like variable shadowing.

**Decision**:  
Enrich the Reviewer prompt with:
1.  **Full Contents** of every file changed in the PR.
2.  **Related Dependencies** fetched via the Scout pass.
3.  **Repository Structure** (filtered file tree) to inform architectural context.
4.  **Last 10 Commits** to help the model understand the progression and intent of the changes.

**Rationale**:  
- Enables the model to find bugs that exist *around* the changes but aren't visible in a standard +/- diff.
- Allows for high-level architectural feedback (e.g., "This new helper already exists in `utils/`").

---

## ADR 4: Robust Defect Extraction & General Comments fallback
**Context**:  
LLM outputs can be unpredictable (e.g., wrapping JSON in markdown or formatting it as a naked array). Furthermore, comments on unchanged lines cause API errors.

**Decision**:  
- Implement robust JSON extraction logic (ignoring markdown wrappers).
- Create a `PostGeneralComment` fallback. If an inline comment fails validation because the line number is outside the diff, retry by posting it as a general PR comment.

**Rationale**:  
- Ensures no valuable feedback is lost due to formatting quirks or GitHub API restrictions.
- Enhances user experience by providing a consolidated "global" feedback section for cross-file issues.
