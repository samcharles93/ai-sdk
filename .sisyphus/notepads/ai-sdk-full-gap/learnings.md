## Cohere Provider Implementation (T16)

### Implementation Notes
- Single Provider struct implements `chat.Provider`, `embed.Provider`, `rerank.Provider`
- Cohere uses its own API format (NOT OpenAI-compatible): message + chat_history split, uppercase roles, parameter_definitions for tools
- Chat: last USER message goes in `message` field, all others in `chat_history` array
- Roles: `chat.RoleSystem` → `"SYSTEM"`, `chat.RoleUser` → `"USER"`, `chat.RoleAssistant` → `"CHATBOT"`, `chat.RoleTool` → `"TOOL"`
- Tool calls in response: parameters are JSON objects (not strings like OpenAI) — marshalled to JSON string for canonical `chat.ToolCall.Arguments`
- Finish reason mapping: `COMPLETE` → `"stop"`, `MAX_TOKENS` → `"length"`, `ERROR`/`ERROR_TOXIC`/`ERROR_LIMIT` → `""`
- Streaming: SSE with event types (text-generation, tool-calls-generation, stream-end)
- Embed: includes `input_type=search_document` by default
- Rerank: results include index + relevance_score, documents reconstructed from original request
- Stream field uses `json:"stream"` (no omitempty) to always include it in requests
