# LLM Context Strategy

This document details the multi-pass context strategy used by the AI Code Reviewer to ensure high-quality, defect-focused analysis with zero false positives.

## 🌉 Overview: The Two-Pass System

To overcome "context blindness," the system uses a two-step process:
1. **Scout Pass**: Identifies which external files (dependencies, types, interfaces) are needed to understand a change.
2. **Review Pass**: Performs the actual analysis using the gathered context.

---

## 🔭 Pass 1: The Scout (Dependency Discovery)

The Scout pass ensures the reviewer doesn't just see *what* changed, but *how* it connects to the rest of the codebase.

### Data Points Included:
| Component | Purpose |
| :--- | :--- |
| **Git Diff** | Minimal view of the changes to identify entry points. |
| **Changed Filenames** | List of modified files to anchor the search. |
| **Repository Structure** | Full directory tree (output of `find .`) so the LLM knows what files exist to request. |

### Logic:
The Scout analyzes imports and function calls in the diff, then requests up to **12 additional files** (interfaces, deep type definitions, base classes) from the repository structure.

---

## 🧠 Pass 2: The Review (Detailed Analysis)

The Review pass is the "heavy lifter," provided with a massive context window (up to 400,000 characters) to verify logic across file boundaries.

### 1. PR Metadata (Developer Intent)
- **Title & Description**: Helps distinguish between intentional debugging code vs. accidental security leaks.
- **Commit History**: Shows the "story" of the PR to understand if a change is a final fix or a temporary workaround.

### 2. Annotated Changed Files
Instead of just sending a diff, we send the **Full File Content** with inline annotations:
```javascript
/* Change Block 1:
- oldCode();
+ newCode_WithBug();
*/
```
- **Why**: Allows the LLM to see the surrounding scope (variables, imports) while focusing its comments strictly on the lines that actually changed.

### 3. Supporting Context (Dependencies)
- **Full Content of Related Files**: The files identified by the Scout (e.g., the struct definition in `models/` when a field is used in `handlers/`).
- **Why**: critical for verifying type safety and interface compliance.

### 4. Repository Structure
- **Full Tree**: Provides architectural context (e.g., "Is this file in the correct package?").

### 5. Detection Layers & Constraints
- **Active Layers**: Dynamically injected based on dashboard settings (Security, Performance, etc.).
- **Strict Compliance Rules**: Mandatory system instructions to:
    - **Never** provide praise or "Looks good" comments.
    - **Limit** comments to 2 concise sentences.
    - **Verify** line numbers strictly against the diff.

---

## 🛡️ Noise Reduction Strategy
- **Pre-Filtering**: Documentation (`.md`, `LICENSE`), configuration (`package.json`, `go.mod`), and static assets are stripped before the LLM even sees them.
- **Intent Awareness**: If a PR description says "temporary debug logging," the LLM is instructed not to flag the logs as security issues unless they expose sensitive data.
