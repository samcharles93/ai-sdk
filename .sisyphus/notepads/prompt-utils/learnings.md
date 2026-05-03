### learnings

- Implemented a small prompt utilities package with no external deps.
- PromptTemplate.Render uses a simple regexp to replace {var} placeholders.
- SystemPrompt builder appends a Constraints: section when provided.
- FormatMessages accepts []map[string]any and prefers "content" then "parts".
