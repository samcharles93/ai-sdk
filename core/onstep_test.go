package core

import (
	"context"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

// TestGenerateText_OnStepRunsBetweenSteps checks the seam: the hook fires
// before the second model call (not the first), sees the accumulated history,
// and its replacement is what the next call receives.
func TestGenerateText_OnStepRunsBetweenSteps(t *testing.T) {
	calc := NewTool("calc", "", nil, func(context.Context, string) (string, error) {
		return "5", nil
	})
	p := &fakeProvider{
		chatScript: []chat.Response{
			{ToolCalls: []chat.ToolCall{{ID: "call_1", Name: "calc", Arguments: `{}`}}, FinishReason: "tool_calls"},
			{Content: "done", FinishReason: "stop"},
		},
	}

	var called int
	var sawHistory []chat.Message
	_, err := GenerateText(context.Background(), p, GenerateOptions{
		Model: "m", Prompt: "x", Tools: ToolSet{"calc": calc}, MaxSteps: 3,
		OnStep: func(ctx context.Context, messages []chat.Message) ([]chat.Message, error) {
			called++
			sawHistory = messages
			return []chat.Message{{Role: chat.RoleUser, Content: "compacted"}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("OnStep called %d times, want 1", called)
	}
	// The history the hook saw includes the prompt, the assistant tool call
	// and the tool result from step 0.
	if len(sawHistory) < 3 {
		t.Fatalf("expected >=3 history messages, got %d: %+v", len(sawHistory), sawHistory)
	}
	// The second model call received the replaced history.
	if len(p.chatCalls) != 2 {
		t.Fatalf("calls = %d", len(p.chatCalls))
	}
	second := p.chatCalls[1].Messages
	if len(second) != 1 || second[0].Content != "compacted" {
		t.Fatalf("second call messages = %+v, want the compacted marker", second)
	}
}

// TestGenerateText_OnStepNotCalledBeforeFirstCall checks the hook is skipped
// for the initial call.
func TestGenerateText_OnStepNotCalledBeforeFirstCall(t *testing.T) {
	p := &fakeProvider{
		chatScript: []chat.Response{{Content: "done", FinishReason: "stop"}},
	}
	var called int
	_, err := GenerateText(context.Background(), p, GenerateOptions{
		Model: "m", Prompt: "x",
		OnStep: func(context.Context, []chat.Message) ([]chat.Message, error) {
			called++
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatalf("OnStep called %d times on a single-step run, want 0", called)
	}
}

// TestGenerateText_OnStepNilKeepsHistory checks returning nil leaves the
// history untouched for the next call.
func TestGenerateText_OnStepNilKeepsHistory(t *testing.T) {
	calc := NewTool("calc", "", nil, func(context.Context, string) (string, error) {
		return "5", nil
	})
	p := &fakeProvider{
		chatScript: []chat.Response{
			{ToolCalls: []chat.ToolCall{{ID: "call_1", Name: "calc", Arguments: `{}`}}, FinishReason: "tool_calls"},
			{Content: "done", FinishReason: "stop"},
		},
	}
	_, err := GenerateText(context.Background(), p, GenerateOptions{
		Model: "m", Prompt: "x", Tools: ToolSet{"calc": calc}, MaxSteps: 3,
		OnStep: func(context.Context, []chat.Message) ([]chat.Message, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	second := p.chatCalls[1].Messages
	if len(second) != 3 {
		t.Fatalf("expected 3 messages (prompt, assistant, tool), got %d: %+v", len(second), second)
	}
}
