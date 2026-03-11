# Custom Review Rules (Developer Rules)

You can customize how the AI reviewer behaves in your repository by defining a `review_rules` block in your `.github/workflows/ai-review.yml` file. This allows you to enforce project-specific conventions, skip specific files, or tell the AI to focus on high-risk areas.

## How to use

Add the `review_rules` input to your workflow file under the `with:` section:

```yaml
- name: Run AI Reviewer
  uses: appointytech/ai-code-reviewer@main # Use your org's path here
  with:
    gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
    github_token: ${{ secrets.GITHUB_TOKEN }}
    review_rules: |
      instructions:
        - "Use chi router for all new HTTP handlers"
        - "All database queries must use sqlc-generated code"
        - "Error messages should not expose internal paths to users"
      ignore:
        - "**/*.pb.go"
        - "internal/legacy/**"
        - "scripts/**"
      focus:
        - "Authentication logic in internal/auth/"
        - "Security boundaries in public API handlers"
```

## Rule Sections

### 1. `instructions`
These are free-form strings that are injected into the LLM's prompt with **high priority**. Use them to define coding standards, framework choices, or specific "dos and don'ts" for your project.

**Examples:**
- `"This is a local project, ignore debug logs and print statements"`
- `"Use Chi for routing, avoid using standard library http.ServeMux directly"`
- `"Ensure all map accesses check for existence first"`

### 2. `ignore` (File Filtering)
Uses standard glob patterns to exclude files from the review entirely. Files matching these patterns are filtered out at the CLI level before they are even sent to the AI, saving tokens and time.

**Examples:**
- `ignore: ["**/*_test.go"]` — Skip all test files
- `ignore: ["internal/handlers/legacy_handler.go"]` — Skip a specific problematic file
- `ignore: ["api/proto/**"]` — Skip all generated proto files

### 3. `focus`
Tells the AI to pay extra attention to certain areas. While the AI still reviews the whole diff, it will spend more effort analyzing logic in these paths or categories.

**Examples:**
- `"Input validation on all public endpoints"`
- `"Concurrency and race conditions in the internal/queue package"`

## Safety & Security Guards

To prevent accidental behavior overrides, the system includes a **surgical sanitization** layer:

1.  **Strict Security Priority**: If an instruction explicitly tells the AI to `"Ignore all security issues"` or `"Skip all bugs"`, that specific part of the instruction is stripped before it reaches the LLM.
2.  **Jailbreak Protection**: Instructions like `"forget previous commands"` or `"you are now a helpful assistant"` are blocked.
3.  **Domain Rules Allowed**: Legitimate instructions like `"ignore debug logs for local dev"` or `"skip documentation checks"` are fully supported.
4.  **Length Limits**: Rules are capped at 4,000 characters to prevent prompt stuffing.
