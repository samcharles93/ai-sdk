package sse

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/pkg/core"
	"github.com/samcharles93/ai-sdk/pkg/uimessage"
)

func TestWriterSerialisesChunks(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriterTo(&buf)
	chunks := []uimessage.Chunk{
		uimessage.StartChunk{MessageID: "m1"},
		uimessage.TextStartChunk{ID: "t"},
		uimessage.TextDeltaChunk{ID: "t", Delta: "Hi"},
		uimessage.TextEndChunk{ID: "t"},
		uimessage.FinishChunk{FinishReason: uimessage.FinishReasonStop},
	}
	for _, c := range chunks {
		if err := w.WriteChunk(c); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
	}
	body := buf.String()
	if !strings.Contains(body, `data: {"type":"start","messageId":"m1"}`) {
		t.Errorf("missing start: %s", body)
	}
	events := SSEEventLines(body)
	if len(events) != len(chunks) {
		t.Fatalf("events=%d, want %d", len(events), len(chunks))
	}
	// First event should be the start chunk and re-decode cleanly.
	c, err := uimessage.UnmarshalChunk(json.RawMessage(events[0]))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := c.(uimessage.StartChunk); !ok {
		t.Errorf("got %T", c)
	}
}

func TestFromTextStreamHappyPath(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartTextDelta, TextDelta: "Hel"}
	ch <- core.StreamPart{Type: core.StreamPartTextDelta, TextDelta: "lo"}
	ch <- core.StreamPart{Type: core.StreamPartFinishStep}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out := FromTextStream(ctx, stream, "m1")

	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}
	want := []string{
		"start", "start-step",
		"text-start", "text-delta", "text-delta", "text-end",
		"finish-step", "finish",
	}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d (%v)", len(got), len(want), typeNames(got))
	}
	for i, w := range want {
		if got[i].TypeName() != w {
			t.Errorf("[%d] got %s, want %s", i, got[i].TypeName(), w)
		}
	}
	// Concatenate text deltas.
	var sb strings.Builder
	for _, c := range got {
		if d, ok := c.(uimessage.TextDeltaChunk); ok {
			sb.WriteString(d.Delta)
		}
	}
	if sb.String() != "Hello" {
		t.Errorf("text=%q", sb.String())
	}
}

func TestFromTextStreamToolCall(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartToolCall, ToolCall: &core.ToolCall{
		ToolCallID: "c1", ToolName: "weather", Input: `{"city":"Sydney"}`,
	}}
	ch <- core.StreamPart{Type: core.StreamPartToolResult, ToolResult: &core.ToolResult{
		ToolCallID: "c1", ToolName: "weather", Output: `{"tempC":22}`,
	}}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}
	if !containsType(got, "tool-input-available") || !containsType(got, "tool-output-available") {
		t.Errorf("missing tool chunks: %v", typeNames(got))
	}
	for _, c := range got {
		if tc, ok := c.(uimessage.ToolInputAvailableChunk); ok {
			m, ok := tc.Input.(map[string]any)
			if !ok || m["city"] != "Sydney" {
				t.Errorf("input=%#v", tc.Input)
			}
		}
	}
}

func typeNames(cs []uimessage.Chunk) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.TypeName()
	}
	return out
}

func containsType(cs []uimessage.Chunk, typ string) bool {
	for _, c := range cs {
		if c.TypeName() == typ {
			return true
		}
	}
	return false
}
