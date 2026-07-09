package uimessage

import (
	"reflect"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

func TestToModelMessagesUserText(t *testing.T) {
	in := []Message{
		{ID: "m1", Role: RoleUser, Parts: MessageParts{TextUIPart{Text: "hi"}}},
	}
	got, err := ToModelMessages(in, ToModelOptions{})
	if err != nil {
		t.Fatalf("ToModelMessages: %v", err)
	}
	want := []chat.Message{
		{Role: chat.RoleUser, Parts: chat.Parts{chat.TextPart{Text: "hi"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v want %#v", got, want)
	}
}

func TestToModelMessagesUserImageDataURL(t *testing.T) {
	// "hi" -> base64 "aGk="
	in := []Message{
		{ID: "m1", Role: RoleUser, Parts: MessageParts{
			TextUIPart{Text: "look"},
			FileUIPart{MediaType: "image/png", URL: "data:image/png;base64,aGk=", Filename: "a.png"},
		}},
	}
	got, err := ToModelMessages(in, ToModelOptions{})
	if err != nil {
		t.Fatalf("ToModelMessages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if len(got[0].Parts) != 2 {
		t.Fatalf("parts = %d", len(got[0].Parts))
	}
	ip, ok := got[0].Parts[1].(chat.ImagePart)
	if !ok {
		t.Fatalf("parts[1] = %T", got[0].Parts[1])
	}
	if string(ip.Data) != "hi" || ip.MediaType != "image/png" {
		t.Errorf("image part: %#v", ip)
	}
}

func TestToModelMessagesAssistantToolFlow(t *testing.T) {
	in := []Message{
		{ID: "m1", Role: RoleAssistant, Parts: MessageParts{
			TextUIPart{Text: "Let me check"},
			ToolUIPart{
				ToolName: "weather", ToolCallID: "c1",
				State:  ToolStateOutputAvailable,
				Input:  map[string]any{"city": "Sydney"},
				Output: map[string]any{"tempC": 22},
			},
		}},
	}
	got, err := ToModelMessages(in, ToModelOptions{})
	if err != nil {
		t.Fatalf("ToModelMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (assistant + tool)", len(got))
	}
	if got[0].Role != chat.RoleAssistant || len(got[0].ToolCalls) != 1 {
		t.Errorf("assistant: %#v", got[0])
	}
	if got[1].Role != chat.RoleTool || got[1].ToolCallID != "c1" {
		t.Errorf("tool: %#v", got[1])
	}
}

func TestLastAssistantHelpers(t *testing.T) {
	complete := []Message{
		{ID: "m1", Role: RoleAssistant, Parts: MessageParts{
			ToolUIPart{ToolName: "x", ToolCallID: "c1", State: ToolStateOutputAvailable},
		}},
	}
	if !LastAssistantMessageIsCompleteWithToolCalls(complete) {
		t.Error("complete should report true")
	}
	pending := []Message{
		{ID: "m1", Role: RoleAssistant, Parts: MessageParts{
			ToolUIPart{ToolName: "x", ToolCallID: "c1", State: ToolStateInputAvailable},
		}},
	}
	if LastAssistantMessageIsCompleteWithToolCalls(pending) {
		t.Error("pending should report false")
	}
}
