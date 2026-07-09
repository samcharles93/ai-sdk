package uimessage

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestChunkRoundTrip(t *testing.T) {
	tFalse := false
	tTrue := true
	tests := []struct {
		name string
		c    Chunk
		want string
	}{
		{
			name: "text-start",
			c:    TextStartChunk{ID: "t1"},
			want: `{"type":"text-start","id":"t1"}`,
		},
		{
			name: "text-delta",
			c:    TextDeltaChunk{ID: "t1", Delta: "hello"},
			want: `{"type":"text-delta","id":"t1","delta":"hello"}`,
		},
		{
			name: "tool-input-start",
			c:    ToolInputStartChunk{ToolCallID: "c1", ToolName: "weather", Dynamic: &tFalse},
			want: `{"type":"tool-input-start","toolCallId":"c1","toolName":"weather","dynamic":false}`,
		},
		{
			name: "tool-output-available",
			c:    ToolOutputAvailableChunk{ToolCallID: "c1", Output: map[string]any{"temp": 22}, Preliminary: &tTrue},
			want: `{"type":"tool-output-available","toolCallId":"c1","output":{"temp":22},"preliminary":true}`,
		},
		{
			name: "start-step",
			c:    StartStepChunk{},
			want: `{"type":"start-step"}`,
		},
		{
			name: "data-weather",
			c:    DataChunk{Name: "weather", Data: map[string]any{"city": "Sydney"}},
			want: `{"type":"data-weather","data":{"city":"Sydney"}}`,
		},
		{
			name: "finish",
			c:    FinishChunk{FinishReason: FinishReasonStop},
			want: `{"type":"finish","finishReason":"stop"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalChunk(tt.c)
			if err != nil {
				t.Fatalf("MarshalChunk: %v", err)
			}
			if !jsonEqual(t, string(got), tt.want) {
				t.Fatalf("marshal = %s, want %s", got, tt.want)
			}
			back, err := UnmarshalChunk(got)
			if err != nil {
				t.Fatalf("UnmarshalChunk: %v", err)
			}
			if back.TypeName() != tt.c.TypeName() {
				t.Fatalf("type after round-trip: got %s, want %s", back.TypeName(), tt.c.TypeName())
			}
			// Compare via JSON re-marshal to avoid pointer/map ordering issues.
			gotJSON, _ := MarshalChunk(back)
			if !jsonEqual(t, string(gotJSON), tt.want) {
				t.Fatalf("round-trip = %s, want %s", gotJSON, tt.want)
			}
		})
	}
}

func TestUnmarshalUnknownType(t *testing.T) {
	if _, err := UnmarshalChunk([]byte(`{"type":"nope"}`)); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestDataChunkPreservesName(t *testing.T) {
	raw := []byte(`{"type":"data-weather","id":"w1","data":{"city":"Sydney"}}`)
	c, err := UnmarshalChunk(raw)
	if err != nil {
		t.Fatalf("UnmarshalChunk: %v", err)
	}
	d, ok := c.(DataChunk)
	if !ok {
		t.Fatalf("got %T, want DataChunk", c)
	}
	if d.Name != "weather" || d.ID != "w1" {
		t.Fatalf("got %#v", d)
	}
}

func jsonEqual(t *testing.T, a, b string) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal([]byte(a), &av); err != nil {
		t.Fatalf("invalid JSON a: %v: %s", err, a)
	}
	if err := json.Unmarshal([]byte(b), &bv); err != nil {
		t.Fatalf("invalid JSON b: %v: %s", err, b)
	}
	return reflect.DeepEqual(av, bv)
}
