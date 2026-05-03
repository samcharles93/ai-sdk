## Cohere Provider — Design Decisions

### Single Provider Struct
All three interfaces (chat, embed, rerank) implemented by one struct. Cohere offers all three capabilities under one API key, so a single provider is appropriate.

### chat_history vs message split
Cohere's API requires the current user message in `message` and all prior conversation in `chat_history`. This required finding the last USER message and extracting it. This is different from OpenAI/other providers that use a flat messages array.

### Parameters as JSON object
Cohere returns tool call parameters as JSON objects (not JSON strings). The provider marshals them back to JSON string for the canonical `chat.ToolCall.Arguments` format to maintain consistency with the domain types.

### stream field
Using `json:"stream"` (no omitempty) because Cohere's non-streaming endpoint behaves differently when the field is absent vs explicitly false. Always including it ensures consistent behavior.
