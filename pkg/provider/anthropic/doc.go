// Package anthropic implements the chat.Provider interface for the Anthropic
// Messages API.
//
// The Anthropic Messages API is a non-OpenAI-compatible endpoint at
// POST /v1/messages that uses a content-block–based wire format with
// distinct shapes for text, image, tool_use, and tool_result blocks.
// Streaming uses SSE with typed events (message_start, content_block_delta,
// message_stop, etc.).
//
// Usage:
//
//	p, err := anthropic.New(anthropic.Config{APIKey: "sk-ant-..."})
//	if err != nil { ... }
//	resp, err := p.Chat(ctx, chat.Request{
//	    Model:    "claude-sonnet-4-20250514",
//	    Messages: []chat.Message{{Role: chat.RoleUser, Content: "hello"}},
//	})
//
// This package implements only the chat portion of the Anthropic API;
// embeddings and other capabilities are not provided by Anthropic.
package anthropic
