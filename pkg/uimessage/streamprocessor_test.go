package uimessage

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMessagePartsRoundTrip(t *testing.T) {
	approved := true
	msg := Message{
		ID:   "m1",
		Role: RoleAssistant,
		Parts: MessageParts{
			TextUIPart{Text: "Hello", State: PartStateDone},
			ReasoningUIPart{Text: "think", State: PartStateDone},
			ToolUIPart{
				ToolName: "weather", ToolCallID: "c1",
				State:    ToolStateOutputAvailable,
				Input:    map[string]any{"city": "Sydney"},
				Output:   map[string]any{"tempC": 22},
				Approval: &ToolApproval{ID: "a1", Approved: &approved},
			},
			DynamicToolUIPart{
				ToolName: "calc", ToolCallID: "c2",
				State: ToolStateInputAvailable,
				Input: "1+2",
			},
			DataUIPart{Name: "alert", ID: "x", Data: map[string]any{"level": "warn"}},
			FileUIPart{MediaType: "image/png", URL: "https://example.com/a.png"},
			StepStartUIPart{},
		},
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Message
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got.Parts) != len(msg.Parts) {
		t.Fatalf("parts len: got %d, want %d", len(got.Parts), len(msg.Parts))
	}
	for i := range got.Parts {
		if got.Parts[i].PartType() != msg.Parts[i].PartType() {
			t.Errorf("parts[%d] type: got %s, want %s", i,
				got.Parts[i].PartType(), msg.Parts[i].PartType())
		}
	}
	// Verify tool name preserved through wire.
	tp, ok := got.Parts[2].(ToolUIPart)
	if !ok {
		t.Fatalf("parts[2] = %T, want ToolUIPart", got.Parts[2])
	}
	if tp.ToolName != "weather" {
		t.Errorf("tool name lost: got %q", tp.ToolName)
	}
	// Verify data part name preserved.
	dp, ok := got.Parts[4].(DataUIPart)
	if !ok {
		t.Fatalf("parts[4] = %T", got.Parts[4])
	}
	if dp.Name != "alert" || dp.ID != "x" {
		t.Errorf("data part: %#v", dp)
	}
}

func TestStreamProcessorTextSequence(t *testing.T) {
	sp := NewStreamProcessor(nil, "m1")
	chunks := []Chunk{
		StartChunk{MessageID: "m-server"},
		StartStepChunk{},
		TextStartChunk{ID: "t1"},
		TextDeltaChunk{ID: "t1", Delta: "Hel"},
		TextDeltaChunk{ID: "t1", Delta: "lo"},
		TextEndChunk{ID: "t1"},
		FinishStepChunk{},
		FinishChunk{FinishReason: FinishReasonStop},
	}
	for i, c := range chunks {
		if err := sp.Apply(c); err != nil {
			t.Fatalf("chunk %d (%s): %v", i, c.TypeName(), err)
		}
	}
	if sp.Message.ID != "m-server" {
		t.Errorf("message id: got %q, want m-server", sp.Message.ID)
	}
	if got := sp.Message.Text(); got != "Hello" {
		t.Errorf("text: got %q, want Hello", got)
	}
	if got := sp.FinishReason(); got != FinishReasonStop {
		t.Errorf("finish reason: got %q, want stop", got)
	}
}

func TestStreamProcessorToolFlow(t *testing.T) {
	sp := NewStreamProcessor(nil, "m1")
	chunks := []Chunk{
		ToolInputStartChunk{ToolCallID: "c1", ToolName: "weather"},
		ToolInputAvailableChunk{ToolCallID: "c1", ToolName: "weather", Input: map[string]any{"city": "Sydney"}},
		ToolOutputAvailableChunk{ToolCallID: "c1", Output: map[string]any{"tempC": 22}},
	}
	var sawCall bool
	sp.OnToolCall = func(_ ToolInputAvailableChunk) error {
		sawCall = true
		return nil
	}
	for i, c := range chunks {
		if err := sp.Apply(c); err != nil {
			t.Fatalf("chunk %d: %v", i, err)
		}
	}
	if !sawCall {
		t.Error("OnToolCall not invoked")
	}
	if len(sp.Message.Parts) != 1 {
		t.Fatalf("parts: %d, want 1", len(sp.Message.Parts))
	}
	tp, ok := sp.Message.Parts[0].(ToolUIPart)
	if !ok {
		t.Fatalf("parts[0] = %T", sp.Message.Parts[0])
	}
	if tp.State != ToolStateOutputAvailable {
		t.Errorf("state = %q", tp.State)
	}
	if !reflect.DeepEqual(tp.Output, map[string]any{"tempC": 22}) {
		t.Errorf("output = %#v", tp.Output)
	}
	if !reflect.DeepEqual(tp.Input, map[string]any{"city": "Sydney"}) {
		t.Errorf("input lost: %#v", tp.Input)
	}
}

func TestStreamProcessorErrorChunk(t *testing.T) {
	sp := NewStreamProcessor(nil, "m1")
	var seen string
	sp.OnError = func(err error) { seen = err.Error() }
	if err := sp.Apply(ErrorChunk{ErrorText: "boom"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if seen != "boom" {
		t.Errorf("OnError got %q", seen)
	}
}

func TestStreamProcessorMissingTextStart(t *testing.T) {
	sp := NewStreamProcessor(nil, "m1")
	err := sp.Apply(TextDeltaChunk{ID: "x", Delta: "a"})
	if err == nil {
		t.Fatal("expected error")
	}
}
