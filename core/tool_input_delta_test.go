package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

// stubTool is a no-op tool that just needs to exist so the loop will
// execute the call; these tests assert on the stream, not the result.
func stubTool(name string) *Tool {
	return NewTool(name, "test tool", json.RawMessage(`{"type":"object"}`),
		func(_ context.Context, _ string) (string, error) { return "ok", nil })
}

// collectParts drains a StreamResult's FullStream into a slice.
func collectParts(t *testing.T, r StreamResult) []StreamPart {
	t.Helper()
	var parts []StreamPart
	for p := range r.FullStream {
		parts = append(parts, p)
	}
	return parts
}

// inputDeltasFor returns the argument fragments emitted for one tool call ID,
// in stream order.
func inputDeltasFor(parts []StreamPart, callID string) []string {
	var out []string
	for _, p := range parts {
		if p.Type == StreamPartToolInputDelta && p.ToolCallID == callID {
			out = append(out, p.ToolInputDelta)
		}
	}
	return out
}

// TestStreamText_ToolInputDeltasStreamed covers the streaming case: a provider
// dribbling one call's arguments across chunks produces ordered fragments that
// concatenate back to the assembled Input, all before the assembled call.
func TestStreamText_ToolInputDeltasStreamed(t *testing.T) {
	chunks := []chat.Chunk{
		{ToolCallDeltas: []chat.ToolCallDelta{{Index: 0, ID: "call_a", Name: "write", ArgsDelta: `{"path":`}}},
		{ToolCallDeltas: []chat.ToolCallDelta{{Index: 0, ArgsDelta: `"a.txt",`}}},
		{ToolCallDeltas: []chat.ToolCallDelta{{Index: 0, ArgsDelta: `"body":"hi"}`}}},
		{Done: true, FinishReason: "tool_calls"},
	}
	p := &fakeProvider{streamScript: [][]chat.Chunk{chunks, {{Delta: "done", Done: true, FinishReason: "stop"}}}}

	r, err := StreamText(t.Context(), p, GenerateOptions{
		Model:  "m",
		Prompt: "x",
		Tools:  ToolSet{"write": stubTool("write")},
	})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	parts := collectParts(t, r)

	got := inputDeltasFor(parts, "call_a")
	want := []string{`{"path":`, `"a.txt",`, `"body":"hi"}`}
	if len(got) != len(want) {
		t.Fatalf("got %d input deltas, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("delta %d = %q, want %q", i, got[i], want[i])
		}
	}

	// The assembled call must be unchanged and must reproduce the fragments.
	call := assembledCall(t, parts, "call_a")
	if joined := strings.Join(got, ""); joined != call.Input {
		t.Errorf("concatenated deltas = %q, want assembled Input %q", joined, call.Input)
	}
	if call.ToolName != "write" {
		t.Errorf("assembled ToolName = %q, want %q", call.ToolName, "write")
	}

	assertDeltasPrecedeCall(t, parts, "call_a")
}

// TestStreamText_ToolInputDeltaSingleChunk covers a provider that emits a
// complete call in one delta (Ollama-style): exactly one fragment, one call.
func TestStreamText_ToolInputDeltaSingleChunk(t *testing.T) {
	chunks := []chat.Chunk{
		{ToolCallDeltas: []chat.ToolCallDelta{{Index: 0, ID: "call_a", Name: "write", ArgsDelta: `{"path":"a.txt"}`}}},
		{Done: true, FinishReason: "tool_calls"},
	}
	p := &fakeProvider{streamScript: [][]chat.Chunk{chunks, {{Delta: "done", Done: true, FinishReason: "stop"}}}}

	r, err := StreamText(t.Context(), p, GenerateOptions{
		Model:  "m",
		Prompt: "x",
		Tools:  ToolSet{"write": stubTool("write")},
	})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	parts := collectParts(t, r)

	got := inputDeltasFor(parts, "call_a")
	if len(got) != 1 {
		t.Fatalf("got %d input deltas, want exactly 1: %q", len(got), got)
	}
	call := assembledCall(t, parts, "call_a")
	if got[0] != call.Input {
		t.Errorf("delta = %q, want assembled Input %q", got[0], call.Input)
	}
	assertDeltasPrecedeCall(t, parts, "call_a")
}

// TestStreamText_ToolInputDeltaSyntheticID pins the fallback: a provider that
// never supplies an ID must tag fragments with the same synthetic call_<index>
// that AssembleToolCalls invents, or fragments cannot be correlated to a call.
func TestStreamText_ToolInputDeltaSyntheticID(t *testing.T) {
	chunks := []chat.Chunk{
		{ToolCallDeltas: []chat.ToolCallDelta{{Index: 0, Name: "write", ArgsDelta: `{"path":`}}},
		{ToolCallDeltas: []chat.ToolCallDelta{{Index: 0, ArgsDelta: `"a.txt"}`}}},
		{Done: true, FinishReason: "tool_calls"},
	}
	p := &fakeProvider{streamScript: [][]chat.Chunk{chunks, {{Delta: "done", Done: true, FinishReason: "stop"}}}}

	r, err := StreamText(t.Context(), p, GenerateOptions{
		Model:  "m",
		Prompt: "x",
		Tools:  ToolSet{"write": stubTool("write")},
	})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	parts := collectParts(t, r)

	call := assembledCall(t, parts, "call_0")
	got := inputDeltasFor(parts, "call_0")
	if joined := strings.Join(got, ""); joined != call.Input {
		t.Errorf("concatenated deltas = %q, want assembled Input %q", joined, call.Input)
	}
}

// TestStreamText_ToolInputDeltasParallelCalls checks that fragments for
// concurrent calls stay attributed to the right call ID.
func TestStreamText_ToolInputDeltasParallelCalls(t *testing.T) {
	chunks := []chat.Chunk{
		{ToolCallDeltas: []chat.ToolCallDelta{
			{Index: 0, ID: "call_a", Name: "write", ArgsDelta: `{"p":`},
			{Index: 1, ID: "call_b", Name: "read", ArgsDelta: `{"q":`},
		}},
		{ToolCallDeltas: []chat.ToolCallDelta{
			{Index: 1, ArgsDelta: `"b"}`},
			{Index: 0, ArgsDelta: `"a"}`},
		}},
		{Done: true, FinishReason: "tool_calls"},
	}
	p := &fakeProvider{streamScript: [][]chat.Chunk{chunks, {{Delta: "done", Done: true, FinishReason: "stop"}}}}

	r, err := StreamText(t.Context(), p, GenerateOptions{
		Model:  "m",
		Prompt: "x",
		Tools:  ToolSet{"write": stubTool("write"), "read": stubTool("read")},
	})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	parts := collectParts(t, r)

	for _, id := range []string{"call_a", "call_b"} {
		call := assembledCall(t, parts, id)
		got := strings.Join(inputDeltasFor(parts, id), "")
		if got != call.Input {
			t.Errorf("%s: concatenated deltas = %q, want assembled Input %q", id, got, call.Input)
		}
		assertDeltasPrecedeCall(t, parts, id)
	}
}

// TestStreamText_NoToolInputDeltasWithoutTools guards existing behaviour: a
// plain text stream emits no tool-input-delta parts at all.
func TestStreamText_NoToolInputDeltasWithoutTools(t *testing.T) {
	p := &fakeProvider{streamScript: [][]chat.Chunk{{{Delta: "hello", Done: true, FinishReason: "stop"}}}}
	r, err := StreamText(t.Context(), p, GenerateOptions{Model: "m", Prompt: "x"})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	for _, part := range collectParts(t, r) {
		if part.Type == StreamPartToolInputDelta {
			t.Fatalf("unexpected tool-input-delta on a text-only stream: %+v", part)
		}
	}
}

// assembledCall finds the StreamPartToolCall for id and fails if absent.
func assembledCall(t *testing.T, parts []StreamPart, id string) ToolCall {
	t.Helper()
	for _, p := range parts {
		if p.Type == StreamPartToolCall && p.ToolCall != nil && p.ToolCall.ToolCallID == id {
			return *p.ToolCall
		}
	}
	t.Fatalf("no assembled tool call for id %q", id)
	return ToolCall{}
}

// assertDeltasPrecedeCall checks every fragment for id arrives before the
// assembled call for id.
func assertDeltasPrecedeCall(t *testing.T, parts []StreamPart, id string) {
	t.Helper()
	callAt := -1
	for i, p := range parts {
		if p.Type == StreamPartToolCall && p.ToolCall != nil && p.ToolCall.ToolCallID == id {
			callAt = i
			break
		}
	}
	if callAt < 0 {
		t.Fatalf("no assembled tool call for id %q", id)
	}
	for i, p := range parts {
		if p.Type == StreamPartToolInputDelta && p.ToolCallID == id && i > callAt {
			t.Errorf("input delta at %d arrived after the assembled call at %d", i, callAt)
		}
	}
}
