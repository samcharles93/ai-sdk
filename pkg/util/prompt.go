package util

import (
	"strings"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

// FormatMessages formats a slice of chat.Message into a human-readable
// string useful for logging or developer display.
func FormatMessages(messages []chat.Message) string {
	var b strings.Builder
	for i, m := range messages {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		b.WriteString(string(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Text())
	}
	return b.String()
}

// SystemPrompt creates a system role chat.Message with the provided
// instructions.
func SystemPrompt(instructions string) chat.Message {
	return chat.Message{Role: chat.RoleSystem, Content: instructions}
}

// UserPrompt creates a user role chat.Message with the provided text.
func UserPrompt(text string) chat.Message {
	return chat.Message{Role: chat.RoleUser, Content: text}
}

// AssistantPrompt creates an assistant role chat.Message with the
// provided text.
func AssistantPrompt(text string) chat.Message {
	return chat.Message{Role: chat.RoleAssistant, Content: text}
}

// ToolResultMessage creates a tool (RoleTool) message referencing the
// originating call and carrying the tool output.
func ToolResultMessage(callID, text string) chat.Message {
	return chat.Message{Role: chat.RoleTool, ToolCallID: callID, Content: text}
}
