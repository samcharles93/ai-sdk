package chat

import (
	"context"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/pkg/uimessage"
)

// fakeTransport is a minimal Transport that emits a fixed chunk
// sequence on the returned channel. It is used to drive Chat without
// touching a real provider.
type fakeTransport struct {
	chunks []Chunk
}

func (t *fakeTransport) SendMessages(_ context.Context, _ string, _ []Message, _ SendOptions) (<-chan Chunk, error) {
	out := make(chan Chunk, len(t.chunks))
	for _, c := range t.chunks {
		out <- c
	}
	close(out)
	return out, nil
}

func TestChatSendDrivesProcessor(t *testing.T) {
	tr := &fakeTransport{chunks: []Chunk{
		uimessage.StartChunk{},
		uimessage.StartStepChunk{},
		uimessage.TextStartChunk{ID: "t"},
		uimessage.TextDeltaChunk{ID: "t", Delta: "Hello"},
		uimessage.TextEndChunk{ID: "t"},
		uimessage.FinishStepChunk{},
		uimessage.FinishChunk{FinishReason: uimessage.FinishReasonStop},
	}}
	var finished FinishEvent
	c := New(Options{
		Transport: tr,
		OnFinish:  func(ev FinishEvent) { finished = ev },
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Send(ctx, CreateMessage{Text: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	msgs := c.Messages()
	if len(msgs) != 2 {
		t.Fatalf("messages=%d, want 2", len(msgs))
	}
	if msgs[0].Role != RoleUser {
		t.Errorf("user role: %v", msgs[0].Role)
	}
	if msgs[1].Role != RoleAssistant {
		t.Errorf("assistant role: %v", msgs[1].Role)
	}
	if got := msgs[1].Text(); got != "Hello" {
		t.Errorf("assistant text: %q", got)
	}
	if c.Status() != StatusReady {
		t.Errorf("status: %v", c.Status())
	}
	if finished.FinishReason != uimessage.FinishReasonStop {
		t.Errorf("finish reason: %v", finished.FinishReason)
	}
}

func TestChatRegenerateTruncates(t *testing.T) {
	tr := &fakeTransport{chunks: []Chunk{
		uimessage.StartChunk{},
		uimessage.StartStepChunk{},
		uimessage.TextStartChunk{ID: "t"},
		uimessage.TextDeltaChunk{ID: "t", Delta: "again"},
		uimessage.TextEndChunk{ID: "t"},
		uimessage.FinishChunk{FinishReason: uimessage.FinishReasonStop},
	}}
	c := New(Options{Transport: tr})
	c.SetMessages([]Message{
		{ID: "m1", Role: RoleUser, Parts: MessageParts{uimessage.TextUIPart{Text: "q"}}},
		{ID: "m2", Role: RoleAssistant, Parts: MessageParts{uimessage.TextUIPart{Text: "old"}}},
	})

	if err := c.Regenerate(context.Background(), "m2"); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	msgs := c.Messages()
	if len(msgs) != 2 {
		t.Fatalf("messages=%d, want 2 (user + new assistant)", len(msgs))
	}
	if msgs[1].Text() != "again" {
		t.Errorf("new assistant text: %q", msgs[1].Text())
	}
}

func TestChatTransportError(t *testing.T) {
	c := New(Options{Transport: NewDefaultTransport("/api/chat")})
	if err := c.Send(context.Background(), CreateMessage{Text: "hi"}); err == nil {
		t.Fatal("expected error from default transport stub")
	}
	if c.Status() != StatusError {
		t.Errorf("status: %v", c.Status())
	}
}
