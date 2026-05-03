package sse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/pkg/chat"
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

// TestWriterExhaustiveChunkTypes verifies every concrete chunk type can
// be written and round-tripped.
func TestWriterExhaustiveChunkTypes(t *testing.T) {
	chunks := []uimessage.Chunk{
		uimessage.StartChunk{MessageID: "m1"},
		uimessage.StartStepChunk{},
		uimessage.TextStartChunk{ID: "t1"},
		uimessage.TextDeltaChunk{ID: "t1", Delta: "hello"},
		uimessage.TextEndChunk{ID: "t1"},
		uimessage.ReasoningStartChunk{ID: "r1"},
		uimessage.ReasoningDeltaChunk{ID: "r1", Delta: "thinking"},
		uimessage.ReasoningEndChunk{ID: "r1"},
		uimessage.ToolInputAvailableChunk{ToolCallID: "c1", ToolName: "search", Input: map[string]any{"q": "test"}},
		uimessage.ToolOutputAvailableChunk{ToolCallID: "c1", Output: map[string]any{"result": "ok"}},
		uimessage.ToolOutputErrorChunk{ToolCallID: "c1", ErrorText: "failed"},
		uimessage.ErrorChunk{ErrorText: "something went wrong"},
		uimessage.AbortChunk{},
		uimessage.DataChunk{Name: "warning", Data: map[string]any{"message": "deprecated"}},
		uimessage.FinishStepChunk{},
		uimessage.FinishChunk{FinishReason: uimessage.FinishReasonStop},
	}

	for _, want := range chunks {
		t.Run("roundtrip/"+want.TypeName(), func(t *testing.T) {
			var buf bytes.Buffer
			w := NewWriterTo(&buf)
			if err := w.WriteChunk(want); err != nil {
				t.Fatalf("WriteChunk: %v", err)
			}
			events := SSEEventLines(buf.String())
			if len(events) != 1 {
				t.Fatalf("events=%d, want 1", len(events))
			}
			got, err := uimessage.UnmarshalChunk(json.RawMessage(events[0]))
			if err != nil {
				t.Fatalf("UnmarshalChunk: %v", err)
			}
			if got.TypeName() != want.TypeName() {
				t.Errorf("type mismatch: got %s want %s", got.TypeName(), want.TypeName())
			}
		})
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

// --- Reasoning tests -------------------------------------------------------

// TestFromTextStreamReasoning verifies full reasoning block lifecycle:
// reasoning-start → reasoning-delta → reasoning-end within a step.
func TestFromTextStreamReasoning(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartReasoningDelta, ReasoningDelta: "Let me think..."}
	ch <- core.StreamPart{Type: core.StreamPartReasoningDelta, ReasoningDelta: " about this problem."}
	ch <- core.StreamPart{Type: core.StreamPartFinishStep}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	want := []string{
		"start", "start-step",
		"reasoning-start", "reasoning-delta", "reasoning-delta", "reasoning-end",
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

	// Verify reasoning deltas are concatenated correctly.
	var sb strings.Builder
	for _, c := range got {
		if d, ok := c.(uimessage.ReasoningDeltaChunk); ok {
			sb.WriteString(d.Delta)
		}
	}
	if sb.String() != "Let me think... about this problem." {
		t.Errorf("reasoning=%q", sb.String())
	}
}

// TestFromTextStreamMixedTextReasoning verifies text and reasoning can
// interleave within the same step, each getting its own block with
// distinct IDs.
func TestFromTextStreamMixedTextReasoning(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartReasoningDelta, ReasoningDelta: "Hmm..."}
	ch <- core.StreamPart{Type: core.StreamPartTextDelta, TextDelta: "Answer"}
	ch <- core.StreamPart{Type: core.StreamPartFinishStep}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	if !containsType(got, "reasoning-start") {
		t.Error("missing reasoning-start")
	}
	if !containsType(got, "text-start") {
		t.Error("missing text-start")
	}

	// Verify reasoning and text blocks have distinct IDs.
	var reasoningID, textID string
	for _, c := range got {
		switch c := c.(type) {
		case uimessage.ReasoningDeltaChunk:
			reasoningID = c.ID
		case uimessage.TextDeltaChunk:
			textID = c.ID
		}
	}
	if reasoningID == "" || textID == "" {
		t.Fatalf("missing IDs: reasoning=%q text=%q", reasoningID, textID)
	}
	if reasoningID == textID {
		t.Errorf("reasoning and text IDs must differ: %q", reasoningID)
	}
	if !strings.Contains(reasoningID, "reasoning") {
		t.Errorf("reasoning ID should contain 'reasoning': %s", reasoningID)
	}
	if !strings.Contains(textID, "text") {
		t.Errorf("text ID should contain 'text': %s", textID)
	}
}

// TestFromTextStreamMultiStepReasoning verifies reasoning blocks are
// closed and reopened across steps, with step-scoped IDs.
func TestFromTextStreamMultiStepReasoning(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	// Step 0: reasoning
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartReasoningDelta, ReasoningDelta: "Step0 think"}
	ch <- core.StreamPart{Type: core.StreamPartFinishStep}
	// Step 1: more reasoning
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartReasoningDelta, ReasoningDelta: "Step1 think"}
	ch <- core.StreamPart{Type: core.StreamPartFinishStep}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	// Count reasoning block boundaries.
	var reasoningStarts, reasoningEnds int
	var ids []string
	for _, c := range got {
		switch c := c.(type) {
		case uimessage.ReasoningStartChunk:
			reasoningStarts++
			ids = append(ids, c.ID)
		case uimessage.ReasoningEndChunk:
			reasoningEnds++
		}
	}
	if reasoningStarts != 2 {
		t.Errorf("reasoning-starts=%d, want 2", reasoningStarts)
	}
	if reasoningEnds != 2 {
		t.Errorf("reasoning-ends=%d, want 2", reasoningEnds)
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Errorf("step IDs should differ: %v", ids)
	}
}

// --- Error / abort / warning tests ------------------------------------------

// TestFromTextStreamError verifies an error part produces an error chunk
// and closes the stream.
func TestFromTextStreamError(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartTextDelta, TextDelta: "partial"}
	ch <- core.StreamPart{Type: core.StreamPartError, Error: errors.New("service unavailable")}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	if !containsType(got, "error") {
		t.Errorf("missing error chunk: %v", typeNames(got))
	}
	for _, c := range got {
		if ec, ok := c.(uimessage.ErrorChunk); ok {
			if ec.ErrorText != "service unavailable" {
				t.Errorf("error text=%q", ec.ErrorText)
			}
		}
	}
	// Channel closure triggers finish after inline error event.
	if !containsType(got, "finish") {
		t.Errorf("missing finish after error: %v", typeNames(got))
	}
}

// TestFromTextStreamErrorFromString verifies error propagation when the
// error is provided as ErrorString (wire-format) rather than as Go error.
func TestFromTextStreamErrorFromString(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{
		Type:        core.StreamPartError,
		ErrorString: "rate limit exceeded",
	}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	if !containsType(got, "error") {
		t.Errorf("missing error chunk: %v", typeNames(got))
	}
	for _, c := range got {
		if ec, ok := c.(uimessage.ErrorChunk); ok {
			if ec.ErrorText != "rate limit exceeded" {
				t.Errorf("error text=%q", ec.ErrorText)
			}
		}
	}
}

// TestFromTextStreamAbort verifies an abort part produces an abort chunk
// and closes the stream.
func TestFromTextStreamAbort(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartTextDelta, TextDelta: "interrupted..."}
	ch <- core.StreamPart{Type: core.StreamPartAbort}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	if !containsType(got, "abort") {
		t.Errorf("missing abort chunk: %v", typeNames(got))
	}
	// Channel closure triggers finish after inline abort event.
	if !containsType(got, "finish") {
		t.Errorf("missing finish after abort: %v", typeNames(got))
	}
}

// TestFromTextStreamWarning verifies warnings are surfaced as data chunks.
func TestFromTextStreamWarning(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{
		Type:    core.StreamPartWarning,
		Warning: &chat.Warning{Message: "image part dropped: model is text-only", Type: "unsupported-content"},
	}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	if !containsType(got, "data-warning") {
		t.Errorf("missing data-warning chunk: %v", typeNames(got))
	}
	for _, c := range got {
		if dc, ok := c.(uimessage.DataChunk); ok && dc.Name == "warning" {
			w, ok := dc.Data.(*chat.Warning)
			if !ok {
				t.Fatalf("warning data is %T, want *chat.Warning", dc.Data)
			}
			if w.Message != "image part dropped: model is text-only" {
				t.Errorf("warning message=%q", w.Message)
			}
		}
	}
}

// TestFromTextStreamWarningNil verifies a warning part with nil Warning
// is silently skipped.
func TestFromTextStreamWarningNil(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartWarning, Warning: nil}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	if containsType(got, "data-warning") {
		t.Error("nil warning should not produce data-warning chunk")
	}
}

// TestFromTextStreamErrorNil verifies an error part with nil Error and
// empty ErrorString produces a default error message.
func TestFromTextStreamErrorNil(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartError}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	for _, c := range got {
		if ec, ok := c.(uimessage.ErrorChunk); ok {
			if ec.ErrorText != "stream error" {
				t.Errorf("default error text=%q", ec.ErrorText)
			}
		}
	}
}

// --- Edge case tests --------------------------------------------------------

// TestFromTextStreamEmpty verifies an immediately-closed stream produces
// only start and finish.
func TestFromTextStreamEmpty(t *testing.T) {
	ch := make(chan core.StreamPart)
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	want := []string{"start", "finish"}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d (%v)", len(got), len(want), typeNames(got))
	}
	for i, w := range want {
		if got[i].TypeName() != w {
			t.Errorf("[%d] got %s, want %s", i, got[i].TypeName(), w)
		}
	}
}

// TestFromTextStreamNoFinishReason verifies StreamPartFinish without an
// explicit finish reason still closes the stream.
func TestFromTextStreamNoFinishReason(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartFinish} // empty finish reason
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	last := got[len(got)-1]
	if last.TypeName() != "finish" {
		t.Errorf("last chunk is %s, want finish", last.TypeName())
	}
}

// TestFromTextStreamFinishReasonLength verifies non-stop finish reasons
// propagate correctly.
func TestFromTextStreamFinishReasonLength(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonLength}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	finish, ok := got[len(got)-1].(uimessage.FinishChunk)
	if !ok {
		t.Fatalf("last chunk is %T, want FinishChunk", got[len(got)-1])
	}
	if string(finish.FinishReason) != string(core.FinishReasonLength) {
		t.Errorf("finish reason=%q, want %q", finish.FinishReason, core.FinishReasonLength)
	}
}

// TestFromTextStreamToolCallInvalidJSON verifies a tool call with
// malformed JSON input falls back to the raw string.
func TestFromTextStreamToolCallInvalidJSON(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartToolCall, ToolCall: &core.ToolCall{
		ToolCallID: "c1", ToolName: "echo", Input: `not-json`,
	}}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	for _, c := range got {
		if tc, ok := c.(uimessage.ToolInputAvailableChunk); ok {
			s, ok := tc.Input.(string)
			if !ok || s != "not-json" {
				t.Errorf("input=%#v, want string \"not-json\"", tc.Input)
			}
		}
	}
}

// TestFromTextStreamToolResultInvalidJSON verifies a tool result with
// malformed JSON output falls back to the raw string.
func TestFromTextStreamToolResultInvalidJSON(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartToolCall, ToolCall: &core.ToolCall{
		ToolCallID: "c1", ToolName: "echo", Input: `"ok"`,
	}}
	ch <- core.StreamPart{Type: core.StreamPartToolResult, ToolResult: &core.ToolResult{
		ToolCallID: "c1", ToolName: "echo", Output: `not-json`,
	}}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	for _, c := range got {
		if tc, ok := c.(uimessage.ToolOutputAvailableChunk); ok {
			s, ok := tc.Output.(string)
			if !ok || s != "not-json" {
				t.Errorf("output=%#v, want string \"not-json\"", tc.Output)
			}
		}
	}
}

// TestFromTextStreamToolResultError verifies a tool result with error
// produces ToolOutputErrorChunk instead of ToolOutputAvailableChunk.
func TestFromTextStreamToolResultError(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartToolCall, ToolCall: &core.ToolCall{
		ToolCallID: "c1", ToolName: "risky", Input: "{}",
	}}
	ch <- core.StreamPart{Type: core.StreamPartToolResult, ToolResult: &core.ToolResult{
		ToolCallID: "c1", ToolName: "risky", Error: "operation failed",
	}}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	if containsType(got, "tool-output-available") {
		t.Error("should not have tool-output-available when tool result errors")
	}
	if !containsType(got, "tool-output-error") {
		t.Errorf("missing tool-output-error: %v", typeNames(got))
	}
	for _, c := range got {
		if tc, ok := c.(uimessage.ToolOutputErrorChunk); ok {
			if tc.ErrorText != "operation failed" {
				t.Errorf("error text=%q", tc.ErrorText)
			}
		}
	}
}

// TestFromTextStreamToolCallNil verifies a tool-call part with nil
// ToolCall is silently skipped.
func TestFromTextStreamToolCallNil(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartToolCall, ToolCall: nil}
	ch <- core.StreamPart{Type: core.StreamPartTextDelta, TextDelta: "continued"}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	if containsType(got, "tool-input-available") {
		t.Error("nil ToolCall should not produce tool chunk")
	}
	if !containsType(got, "text-start") {
		t.Error("text should still flow after nil ToolCall skip")
	}
}

// TestFromTextStreamToolResultNil verifies a tool-result part with nil
// ToolResult is silently skipped.
func TestFromTextStreamToolResultNil(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartToolCall, ToolCall: &core.ToolCall{
		ToolCallID: "c1", ToolName: "safe", Input: "{}",
	}}
	ch <- core.StreamPart{Type: core.StreamPartToolResult, ToolResult: nil}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	// Should still have the tool-input-available, but NOT a tool-output chunk.
	if !containsType(got, "tool-input-available") {
		t.Error("missing tool-input-available")
	}
	if containsType(got, "tool-output-available") {
		t.Error("nil ToolResult should not produce tool-output-available")
	}
}

// --- Context cancellation tests ---------------------------------------------

// TestFromTextStreamContextCancel verifies context cancellation
// terminates the stream with an abort chunk.
func TestFromTextStreamContextCancel(t *testing.T) {
	ch := make(chan core.StreamPart) // unbuffered, blocks forever
	stream := &core.StreamResult{FullStream: ch}

	ctx, cancel := context.WithCancel(context.Background())
	out := FromTextStream(ctx, stream, "m1")

	// Read start chunk, then cancel.
	c := <-out
	if c.TypeName() != "start" {
		t.Fatalf("expected start, got %s", c.TypeName())
	}
	cancel()

	// Drain remaining chunks (should be abort or nothing).
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}
	if len(got) > 0 && !containsType(got, "abort") {
		t.Errorf("expected abort after cancel, got: %v", typeNames(got))
	}
}

// TestFromTextStreamSendFailure exercises send-failure paths by filling
// the output buffer then cancelling the context, forcing send() to
// return false and the goroutine to abort mid-stream.
func TestFromTextStreamSendFailure(t *testing.T) {
	ch := make(chan core.StreamPart, 8)

	// Fill ch in a separate goroutine so the test goroutine is not
	// blocked by channel backpressure.
	go func() {
		ch <- core.StreamPart{Type: core.StreamPartStartStep}
		for range 20 {
			ch <- core.StreamPart{Type: core.StreamPartReasoningDelta, ReasoningDelta: "think"}
			ch <- core.StreamPart{Type: core.StreamPartTextDelta, TextDelta: "x"}
		}
		ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
		close(ch)
	}()

	stream := &core.StreamResult{FullStream: ch}
	ctx, cancel := context.WithCancel(context.Background())

	out := FromTextStream(ctx, stream, "m1")

	// Do not drain the output channel — let the goroutine fill the
	// 8-element buffer until send() blocks.
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Drain remaining output.
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}
	if len(got) == 0 {
		t.Error("expected at least some chunks before cancellation")
	}
}

// TestFromTextStreamMultiStepText verifies text blocks close and reopen
// with distinct IDs across multiple steps.
func TestFromTextStreamMultiStepText(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	// Step 0
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartTextDelta, TextDelta: "first"}
	ch <- core.StreamPart{Type: core.StreamPartFinishStep}
	// Step 1
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartTextDelta, TextDelta: "second"}
	ch <- core.StreamPart{Type: core.StreamPartFinishStep}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	// Verify two text blocks, each with start/delta/end and distinct IDs.
	var textStarts, textEnds int
	var ids []string
	for _, c := range got {
		switch c := c.(type) {
		case uimessage.TextStartChunk:
			textStarts++
			ids = append(ids, c.ID)
		case uimessage.TextEndChunk:
			textEnds++
		}
	}
	if textStarts != 2 {
		t.Errorf("text-starts=%d, want 2", textStarts)
	}
	if textEnds != 2 {
		t.Errorf("text-ends=%d, want 2", textEnds)
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Errorf("step text IDs should differ: %v", ids)
	}
}

// TestFromTextStreamNoStepFinish verifies that a direct StreamPartFinish
// closes any open text/reasoning blocks and step boundary.
func TestFromTextStreamNoStepFinish(t *testing.T) {
	ch := make(chan core.StreamPart, 8)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartTextDelta, TextDelta: "direct finish"}
	// No StreamPartFinishStep — stream ends directly.
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	// Should have text-end from closeText(), finish-step from the cleanup,
	// then finish.
	if !containsType(got, "text-end") {
		t.Errorf("missing text-end: %v", typeNames(got))
	}
	if !containsType(got, "finish-step") {
		t.Errorf("missing finish-step: %v", typeNames(got))
	}
	if !containsType(got, "finish") {
		t.Errorf("missing finish: %v", typeNames(got))
	}
}

// TestFromTextStreamStartChunkMetadata verifies message ID is propagated.
func TestFromTextStreamStartChunkMetadata(t *testing.T) {
	ch := make(chan core.StreamPart)
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	out := FromTextStream(context.Background(), stream, "msg-42")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}

	start, ok := got[0].(uimessage.StartChunk)
	if !ok {
		t.Fatalf("first chunk is %T, want StartChunk", got[0])
	}
	if start.MessageID != "msg-42" {
		t.Errorf("messageId=%q, want msg-42", start.MessageID)
	}
}

// --- Writer tests -----------------------------------------------------------

type failingWriter struct {
	limit int
	count int
}

func (w *failingWriter) Write(p []byte) (n int, err error) {
	w.count++
	if w.count > w.limit {
		return 0, errors.New("write failed after limit")
	}
	return len(p), nil
}

func TestApplyHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	ApplyHeaders(w.Header())

	h := w.Header()
	if h.Get("Content-Type") != Headers["Content-Type"] {
		t.Errorf("Content-Type=%q", h.Get("Content-Type"))
	}
	if h.Get("Cache-Control") != Headers["Cache-Control"] {
		t.Errorf("Cache-Control=%q", h.Get("Cache-Control"))
	}
	if h.Get("Connection") != Headers["Connection"] {
		t.Errorf("Connection=%q", h.Get("Connection"))
	}
	if h.Get("X-Vercel-Ai-Ui-Message-Stream") != Headers["X-Vercel-Ai-Ui-Message-Stream"] {
		t.Errorf("X-Vercel-Ai-Ui-Message-Stream=%q", h.Get("X-Vercel-Ai-Ui-Message-Stream"))
	}
	if h.Get("X-Accel-Buffering") != Headers["X-Accel-Buffering"] {
		t.Errorf("X-Accel-Buffering=%q", h.Get("X-Accel-Buffering"))
	}
}

func TestNewWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewWriter(rec)

	if rec.Code != 200 {
		t.Errorf("status=%d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type=%q", rec.Header().Get("Content-Type"))
	}
	// Write a chunk and verify it flushes.
	if err := w.WriteChunk(uimessage.StartChunk{MessageID: "test"}); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if !rec.Flushed {
		t.Error("expected flush after write")
	}
}

func TestWriteRaw(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriterTo(&buf)

	if err := w.WriteRaw(json.RawMessage(`{"key":"value"}`)); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, `data: {"key":"value"}`) {
		t.Errorf("body=%q", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("body should end with double newline: %q", body)
	}
}

func TestWriteRawWithFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewWriter(rec)

	if err := w.WriteRaw(json.RawMessage(`{"flushed":true}`)); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}
	if !rec.Flushed {
		t.Error("expected flush after WriteRaw")
	}
	body := rec.Body.String()
	if !strings.Contains(body, `{"flushed":true}`) {
		t.Errorf("body=%q", body)
	}
}

func TestPipe(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriterTo(&buf)
	src := make(chan uimessage.Chunk, 4)
	src <- uimessage.StartChunk{MessageID: "p1"}
	src <- uimessage.TextDeltaChunk{ID: "t", Delta: "piped"}
	src <- uimessage.FinishChunk{FinishReason: uimessage.FinishReasonStop}
	close(src)

	if err := Pipe(context.Background(), src, w); err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	events := SSEEventLines(buf.String())
	if len(events) != 3 {
		t.Fatalf("events=%d, want 3", len(events))
	}
}

func TestPipeContextCancel(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriterTo(&buf)
	src := make(chan uimessage.Chunk) // unbuffered, blocks

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := Pipe(ctx, src, w)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v, want context.Canceled", err)
	}
}

func TestWriteChunkWriteError(t *testing.T) {
	w := NewWriterTo(&failingWriter{limit: -1})
	err := w.WriteChunk(uimessage.StartChunk{MessageID: "test"})
	if err == nil {
		t.Error("expected error from failing writer")
	}
}

func TestWriteRawWriteError(t *testing.T) {
	w := NewWriterTo(&failingWriter{limit: -1})
	err := w.WriteRaw(json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error from failing writer")
	}
}

func TestPipeWriteError(t *testing.T) {
	w := NewWriterTo(&failingWriter{limit: -1})
	src := make(chan uimessage.Chunk, 2)
	src <- uimessage.StartChunk{MessageID: "p1"}
	src <- uimessage.TextDeltaChunk{ID: "t", Delta: "x"}
	close(src)

	err := Pipe(context.Background(), src, w)
	if err == nil {
		t.Error("expected error from failing writer in Pipe")
	}
}

func TestWriteChunkMarshalError(t *testing.T) {
	w := NewWriterTo(&bytes.Buffer{})
	c := uimessage.DataChunk{Name: "bad", Data: make(chan int)}
	err := w.WriteChunk(c)
	if err == nil {
		t.Error("expected marshal error for unmarshalable data")
	}
}

func TestFromTextStreamPreCancelled(t *testing.T) {
	ch := make(chan core.StreamPart, 4)
	ch <- core.StreamPart{Type: core.StreamPartStartStep}
	ch <- core.StreamPart{Type: core.StreamPartTextDelta, TextDelta: "early"}
	ch <- core.StreamPart{Type: core.StreamPartFinish, FinishReason: core.FinishReasonStop}
	close(ch)

	stream := &core.StreamResult{FullStream: ch}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := FromTextStream(ctx, stream, "m1")
	var got []uimessage.Chunk
	for c := range out {
		got = append(got, c)
	}
	// With pre-cancelled context, the stream should terminate quickly.
	if len(got) > 0 && got[0].TypeName() != "start" {
		t.Errorf("expected start if any chunks, got %s", got[0].TypeName())
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
