package chat

// ---------------------------------------------------------------------------
// Backward-compatible types for modules written against the older
// pkg/ui/chat API surface (e.g., ai-sdk-nats).
//
// These types are thin wrappers / aliases that map to the current
// internal types. They SHOULD NOT be used by new code.
// ---------------------------------------------------------------------------

// UIMessage is a backward-compatible alias for [Message].
type UIMessage = Message

// StreamEvent packages a single part and optional error. It is the
// legacy event type that older transports (e.g., NATS) emit over their
// return channel.
type StreamEvent struct {
	Type  PartType
	Part  UIMessagePart
	Error error
}

// UIMessagePart is a flat, backward-compatible message part. Unlike the
// current polymorphic [uimessage.MessagePart] interface, this type uses
// simple fields for text, reasoning, tool calls, and tool results.
type UIMessagePart struct {
	Type       PartType
	Text       string
	Reasoning  string
	ToolCall   *ToolCall
	ToolResult *ToolResult
}

// PartType enumerates the possible part type constants used in
// [StreamEvent] / [UIMessagePart].
type PartType string

const (
	PartTypeText       PartType = "text"
	PartTypeReasoning  PartType = "reasoning"
	PartTypeToolCall   PartType = "tool-call"
	PartTypeToolResult PartType = "tool-result"
	PartTypeStepStart  PartType = "step-start"
)

// ToolResult holds the outcome of a tool execution. It is the
// backward-compatible counterpart to the newer [uimessage.ToolUIPart]
// / [uimessage.DynamicToolUIPart] parts.
type ToolResult struct {
	ToolCallID string
	ToolName   string
	Output     any
	IsError    bool
}
