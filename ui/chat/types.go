package chat

import (
	"github.com/samcharles93/ai-sdk/uimessage"
)

// Status is the current state of a chat session.
type Status string

const (
	StatusReady     Status = "ready"
	StatusSubmitted Status = "submitted"
	StatusStreaming Status = "streaming"
	StatusError     Status = "error"
)

// Re-exports from pkg/uimessage so that callers of pkg/ui/chat can
// build messages without importing two packages. The protocol types
// live in [uimessage]; this package owns the chat-state behaviour.
type (
	Message      = uimessage.Message
	MessageParts = uimessage.MessageParts
	MessagePart  = uimessage.MessagePart
	Role         = uimessage.Role
	FilePart     = uimessage.FileUIPart
	TextPart     = uimessage.TextUIPart
	Chunk        = uimessage.Chunk
	FinishReason = uimessage.FinishReason
)

// Role constants re-exported from uimessage.
const (
	RoleSystem    = uimessage.RoleSystem
	RoleUser      = uimessage.RoleUser
	RoleAssistant = uimessage.RoleAssistant
)

// CreateMessage describes a message a caller wants to send. Files are
// pre-converted [uimessage.FileUIPart] values; use
// [uimessage.ConvertMultipartFiles] to turn an HTTP multipart upload
// into the right shape.
type CreateMessage struct {
	// Text is the user-authored text body. May be empty when only files
	// are sent.
	Text string

	// Files are attachments to include after the text part.
	Files []uimessage.FileUIPart

	// Metadata is opaque per-message metadata propagated to the
	// transport.
	Metadata any

	// MessageID overrides the auto-generated message ID.
	MessageID string
}
