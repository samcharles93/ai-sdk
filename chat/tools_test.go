package chat

import (
	"strings"
	"testing"
)

func TestAssembleToolCalls_Empty(t *testing.T) {
	if got := AssembleToolCalls(nil); got != nil {
		t.Fatalf("nil deltas: want nil, got %+v", got)
	}
}

func TestAssembleToolCalls_SingleStreamed(t *testing.T) {
	deltas := []ToolCallDelta{
		{Index: 0, ID: "call_abc", Name: "get_weather"},
		{Index: 0, ArgsDelta: `{"city":`},
		{Index: 0, ArgsDelta: `"sydney"`},
		{Index: 0, ArgsDelta: `}`},
	}
	got := AssembleToolCalls(deltas)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d: %+v", len(got), got)
	}
	want := ToolCall{ID: "call_abc", Name: "get_weather", Arguments: `{"city":"sydney"}`}
	if got[0] != want {
		t.Fatalf("want %+v, got %+v", want, got[0])
	}
	if err := ValidateToolCallArguments(got[0].Arguments); err != nil {
		t.Fatalf("invalid args: %v", err)
	}
}

// TestAssembleToolCalls_FirstIDWins covers the OpenAI Responses API delta
// sequence: response.output_item.added carries the authoritative call_id,
// while the function_call_arguments.delta/.done events that follow carry
// only the item id. The later item id must not clobber the call_id, or the
// tool result is encoded against the wrong identifier and the next request
// fails with "No tool output found for function call call_...".
func TestAssembleToolCalls_FirstIDWins(t *testing.T) {
	deltas := []ToolCallDelta{
		{Index: 0, ID: "call_native", Name: "run_shell"},
		{Index: 0, ID: "fc_item", ArgsDelta: `{"command":`},
		{Index: 0, ID: "fc_item", ArgsDelta: `"ls"}`},
		{Index: 0, ID: "fc_item", Name: "run_shell"},
	}
	got := AssembleToolCalls(deltas)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d: %+v", len(got), got)
	}
	want := ToolCall{ID: "call_native", Name: "run_shell", Arguments: `{"command":"ls"}`}
	if got[0] != want {
		t.Fatalf("want %+v, got %+v", want, got[0])
	}
}

// TestAssembleToolCalls_LateIDBackfills covers providers that never announce
// the call up front: the first delta carrying an ID still sets it.
func TestAssembleToolCalls_LateIDBackfills(t *testing.T) {
	deltas := []ToolCallDelta{
		{Index: 0, ArgsDelta: `{"a":1}`},
		{Index: 0, ID: "fc_123", Name: "spawn_agent"},
	}
	got := AssembleToolCalls(deltas)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d: %+v", len(got), got)
	}
	want := ToolCall{ID: "fc_123", Name: "spawn_agent", Arguments: `{"a":1}`}
	if got[0] != want {
		t.Fatalf("want %+v, got %+v", want, got[0])
	}
}

func TestAssembleToolCalls_ParallelOrdered(t *testing.T) {
	// Two parallel tool calls (indices 0 and 1) interleaved in delta order.
	deltas := []ToolCallDelta{
		{Index: 0, ID: "a", Name: "alpha"},
		{Index: 1, ID: "b", Name: "beta"},
		{Index: 1, ArgsDelta: `{"x":2}`},
		{Index: 0, ArgsDelta: `{"x":1}`},
	}
	got := AssembleToolCalls(deltas)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("not sorted by index: %+v", got)
	}
	if got[0].Arguments != `{"x":1}` || got[1].Arguments != `{"x":2}` {
		t.Fatalf("args wrong: %+v", got)
	}
}

func TestAssembleToolCalls_SyntheticID(t *testing.T) {
	deltas := []ToolCallDelta{
		{Index: 0, Name: "f", ArgsDelta: `{}`},
		{Index: 1, Name: "g", ArgsDelta: `{}`},
	}
	got := AssembleToolCalls(deltas)
	if got[0].ID != "call_0" || got[1].ID != "call_1" {
		t.Fatalf("synthetic ids missing: %+v", got)
	}
}

func TestAssembleToolCalls_OutOfOrderIndices(t *testing.T) {
	deltas := []ToolCallDelta{
		{Index: 2, ID: "c", Name: "c", ArgsDelta: `{}`},
		{Index: 0, ID: "a", Name: "a", ArgsDelta: `{}`},
		{Index: 1, ID: "b", Name: "b", ArgsDelta: `{}`},
	}
	got := AssembleToolCalls(deltas)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	for i, want := range []string{"a", "b", "c"} {
		if got[i].ID != want {
			t.Fatalf("position %d: want %s, got %s", i, want, got[i].ID)
		}
	}
}

func TestValidateToolCallArguments(t *testing.T) {
	if err := ValidateToolCallArguments(""); err != nil {
		t.Fatalf("empty: %v", err)
	}
	if err := ValidateToolCallArguments(`{"k":1}`); err != nil {
		t.Fatalf("valid: %v", err)
	}
	err := ValidateToolCallArguments(`{"k":`)
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("invalid: want JSON error, got %v", err)
	}
}
