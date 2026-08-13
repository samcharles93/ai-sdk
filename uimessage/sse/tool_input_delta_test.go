package sse

import (
	"context"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/core"
	"github.com/samcharles93/ai-sdk/uimessage"
)

// drain runs the transform over parts and collects the emitted chunks.
func drain(parts []core.StreamPart) []uimessage.Chunk {
	ch := make(chan core.StreamPart, len(parts))
	for _, p := range parts {
		ch <- p
	}
	close(ch)

	out := FromTextStream(context.Background(), &core.StreamResult{FullStream: ch}, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}
	return got
}

// TestFromTextStreamToolInputDeltas covers the streaming path end to end: the
// fragments core emits become one tool-input-start followed by a
// tool-input-delta each, with the assembled call still arriving as
// tool-input-available.
func TestFromTextStreamToolInputDeltas(t *testing.T) {
	got := drain([]core.StreamPart{
		{Type: core.StreamPartStartStep},
		{Type: core.StreamPartToolInputDelta, ToolCallID: "c1", ToolName: "weather", ToolInputDelta: `{"city":`},
		{Type: core.StreamPartToolInputDelta, ToolCallID: "c1", ToolName: "weather", ToolInputDelta: `"Sydney"}`},
		{Type: core.StreamPartToolCall, ToolCall: &core.ToolCall{
			ToolCallID: "c1", ToolName: "weather", Input: `{"city":"Sydney"}`,
		}},
		{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop},
	})

	// Exactly one start for the call.
	var starts int
	for _, c := range got {
		if s, ok := c.(uimessage.ToolInputStartChunk); ok && s.ToolCallID == "c1" {
			starts++
			if s.ToolName != "weather" {
				t.Errorf("tool-input-start ToolName = %q, want %q", s.ToolName, "weather")
			}
		}
	}
	if starts != 1 {
		t.Errorf("got %d tool-input-start chunks for c1, want exactly 1: %v", starts, typeNames(got))
	}

	// Fragments arrive in order and reconstruct the assembled input.
	var joined strings.Builder
	for _, c := range got {
		if d, ok := c.(uimessage.ToolInputDeltaChunk); ok && d.ToolCallID == "c1" {
			joined.WriteString(d.InputTextDelta)
		}
	}
	if joined.String() != `{"city":"Sydney"}` {
		t.Errorf("concatenated deltas = %q, want %q", joined.String(), `{"city":"Sydney"}`)
	}

	// The assembled call is still delivered unchanged.
	if !containsType(got, "tool-input-available") {
		t.Errorf("missing tool-input-available: %v", typeNames(got))
	}
}

// TestFromTextStreamToolInputDeltaOrdering pins that every fragment precedes
// the assembled call: a UI that renders the input progressively must not
// receive the completed input first.
func TestFromTextStreamToolInputDeltaOrdering(t *testing.T) {
	got := drain([]core.StreamPart{
		{Type: core.StreamPartStartStep},
		{Type: core.StreamPartToolInputDelta, ToolCallID: "c1", ToolName: "weather", ToolInputDelta: `{"city":`},
		{Type: core.StreamPartToolInputDelta, ToolCallID: "c1", ToolName: "weather", ToolInputDelta: `"Sydney"}`},
		{Type: core.StreamPartToolCall, ToolCall: &core.ToolCall{
			ToolCallID: "c1", ToolName: "weather", Input: `{"city":"Sydney"}`,
		}},
		{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop},
	})

	availableAt, startAt := -1, -1
	lastDeltaAt := -1
	for i, c := range got {
		switch v := c.(type) {
		case uimessage.ToolInputStartChunk:
			if startAt < 0 {
				startAt = i
			}
		case uimessage.ToolInputDeltaChunk:
			lastDeltaAt = i
		case uimessage.ToolInputAvailableChunk:
			if v.ToolCallID == "c1" {
				availableAt = i
			}
		}
	}
	if startAt < 0 || lastDeltaAt < 0 || availableAt < 0 {
		t.Fatalf("expected start, delta and available chunks, got: %v", typeNames(got))
	}
	if startAt >= lastDeltaAt || lastDeltaAt >= availableAt {
		t.Errorf("expected start(%d) < delta(%d) < available(%d): %v",
			startAt, lastDeltaAt, availableAt, typeNames(got))
	}
}

// TestFromTextStreamToolInputDeltasParallel checks fragments for concurrent
// calls each open their own block and stay attributed to the right call.
func TestFromTextStreamToolInputDeltasParallel(t *testing.T) {
	got := drain([]core.StreamPart{
		{Type: core.StreamPartStartStep},
		{Type: core.StreamPartToolInputDelta, ToolCallID: "c1", ToolName: "a", ToolInputDelta: `{"x":`},
		{Type: core.StreamPartToolInputDelta, ToolCallID: "c2", ToolName: "b", ToolInputDelta: `{"y":`},
		{Type: core.StreamPartToolInputDelta, ToolCallID: "c2", ToolName: "b", ToolInputDelta: `2}`},
		{Type: core.StreamPartToolInputDelta, ToolCallID: "c1", ToolName: "a", ToolInputDelta: `1}`},
		{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop},
	})

	starts := map[string]int{}
	joined := map[string]string{}
	for _, c := range got {
		switch v := c.(type) {
		case uimessage.ToolInputStartChunk:
			starts[v.ToolCallID]++
		case uimessage.ToolInputDeltaChunk:
			joined[v.ToolCallID] += v.InputTextDelta
		}
	}
	for _, id := range []string{"c1", "c2"} {
		if starts[id] != 1 {
			t.Errorf("%s: got %d start chunks, want 1", id, starts[id])
		}
	}
	if joined["c1"] != `{"x":1}` {
		t.Errorf("c1 deltas = %q, want %q", joined["c1"], `{"x":1}`)
	}
	if joined["c2"] != `{"y":2}` {
		t.Errorf("c2 deltas = %q, want %q", joined["c2"], `{"y":2}`)
	}
}

// TestFromTextStreamToolCallWithoutDeltas guards backward compatibility: a
// provider whose calls arrive only assembled produces no input-start or
// input-delta chunks, exactly as before.
func TestFromTextStreamToolCallWithoutDeltas(t *testing.T) {
	got := drain([]core.StreamPart{
		{Type: core.StreamPartStartStep},
		{Type: core.StreamPartToolCall, ToolCall: &core.ToolCall{
			ToolCallID: "c1", ToolName: "weather", Input: `{"city":"Sydney"}`,
		}},
		{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop},
	})

	if containsType(got, "tool-input-start") || containsType(got, "tool-input-delta") {
		t.Errorf("assembled-only call must not emit input start/delta: %v", typeNames(got))
	}
	if !containsType(got, "tool-input-available") {
		t.Errorf("missing tool-input-available: %v", typeNames(got))
	}
}
