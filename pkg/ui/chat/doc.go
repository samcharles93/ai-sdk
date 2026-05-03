// Package chat provides server-side chat state management — the Go
// equivalent of the AI SDK's useChat() hook. It manages message lists,
// streaming state, and tool output injection, rendering updates via
// Datastar SSE.
//
// Usage:
//
//	chatState := chat.New(chat.Options{
//	    Transport: chat.NewDefaultTransport("/api/chat"),
//	    OnToolCall: func(tc chat.ToolCall) { ... },
//	})
//	chatState.Send(ctx, chat.UserMessage{Text: "Hello"})
package chat
