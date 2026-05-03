package util

import (
	"testing"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

func TestFormatMessages(t *testing.T) {
	msgs := []chat.Message{
		SystemPrompt("set persona"),
		UserPrompt("hello"),
		AssistantPrompt("hi"),
	}
	s := FormatMessages(msgs)
	if s == "" {
		t.Fatal("expected formatted messages, got empty string")
	}
}

func TestToolResultMessage(t *testing.T) {
	m := ToolResultMessage("call_1", "result")
	if m.Role != chat.RoleTool || m.ToolCallID != "call_1" {
		t.Fatalf("unexpected tool message: %+v", m)
	}
}
