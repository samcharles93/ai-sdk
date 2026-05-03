# Anthropic Provider — Learnings

## Architecture
- Provider follows the same structure as `pkg/provider/deepseek/`: Config, Provider, buildBody, Chat, ChatStream
- Compile-time interface assertion: `var _ chat.Provider = (*Provider)(nil)`

## Wire Format Differences
- Anthropic Messages API is NOT OpenAI-compatible
- Content is always an array of content blocks: `[{type: "text", text: "..."}, ...]`
- System prompt is a top-level `system` field (not in messages array)
- Tool results are user messages with `tool_result` blocks (Anthropic has no "tool" role)
- Tool calls are inline in `content` array as `tool_use` blocks
- `stop_reason` maps: end_turn→stop, max_tokens→length, tool_use→tool_calls

## Streaming
- Anthropic uses SSE with explicit `event:` lines (message_start, content_block_start, content_block_delta, content_block_stop, message_delta, message_stop, ping)
- The `event:` lines can be skipped — the `type` field in the JSON data is authoritative
- Content block deltas are typed: text_delta, input_json_delta (for tool args), thinking_delta, signature_delta
- Tool args stream as partial_json fragments; callers concatenate them
- Usage comes in two parts: input_tokens at message_start, output_tokens at message_delta

## Tool Use
- chat.Tool → Anthropic tool def with input_schema (not parameters)
- chat.ToolCall (in assistant) → tool_use content block with id/name/input
- chat.RoleTool message → user message with tool_result content block
- ToolChoiceNone: Anthropic has no equivalent; omit tools from request entirely

## Thinking/Reasoning
- Anthropic thinking blocks store thinking text + signature in ReasoningPart.ProviderMetadata
- Key: `anthropic:thinking_signature` for signature replay across turns

## Headers
- Auth: `x-api-key` (not `Authorization: Bearer`)
- Version: `anthropic-version: 2023-06-01`
- Content-Type: application/json for both streaming and non-streaming
