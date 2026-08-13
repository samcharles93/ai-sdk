package agent

import (
	"testing"

	"github.com/samcharles93/ai-sdk/core"
)

// TestTranslateSkipsUnsurfacedParts pins that parts outside the agent's event
// vocabulary are dropped rather than coerced into an event.
//
// These previously fell through to a default that returned EventFinish, so a
// provider warning — or any part type core later adds, such as tool input
// deltas — appeared to consumers as the run finishing mid-stream.
func TestTranslateSkipsUnsurfacedParts(t *testing.T) {
	unsurfaced := []core.StreamPart{
		{Type: core.StreamPartWarning},
		{Type: core.StreamPartToolInputDelta, ToolCallID: "call_a", ToolInputDelta: `{"a":`},
		{Type: core.StreamPartType("some-future-part")},
	}

	for _, part := range unsurfaced {
		t.Run(string(part.Type), func(t *testing.T) {
			ev, surfaced := translate(part)
			if surfaced {
				t.Errorf("translate(%s) surfaced %s, want it dropped", part.Type, ev.Type)
			}
			if ev.Type == EventFinish {
				t.Errorf("translate(%s) produced a spurious EventFinish", part.Type)
			}
		})
	}
}

// TestTranslateSurfacesKnownParts guards the mapping the agent does expose,
// so the skip path above cannot silently swallow real events.
func TestTranslateSurfacesKnownParts(t *testing.T) {
	cases := []struct {
		part core.StreamPart
		want EventType
	}{
		{core.StreamPart{Type: core.StreamPartTextDelta, TextDelta: "hi"}, EventTextDelta},
		{core.StreamPart{Type: core.StreamPartReasoningDelta, ReasoningDelta: "think"}, EventReasoningDelta},
		{core.StreamPart{Type: core.StreamPartToolCall, ToolCall: &core.ToolCall{}}, EventToolCall},
		{core.StreamPart{Type: core.StreamPartToolResult, ToolResult: &core.ToolResult{}}, EventToolResult},
		{core.StreamPart{Type: core.StreamPartStartStep}, EventStartStep},
		{core.StreamPart{Type: core.StreamPartFinishStep, StepResult: &core.StepResult{}}, EventFinishStep},
		{core.StreamPart{Type: core.StreamPartFinish}, EventFinish},
		{core.StreamPart{Type: core.StreamPartError}, EventError},
		{core.StreamPart{Type: core.StreamPartAbort}, EventAbort},
	}

	for _, tc := range cases {
		t.Run(string(tc.part.Type), func(t *testing.T) {
			ev, surfaced := translate(tc.part)
			if !surfaced {
				t.Fatalf("translate(%s) was dropped, want %s", tc.part.Type, tc.want)
			}
			if ev.Type != tc.want {
				t.Errorf("translate(%s) = %s, want %s", tc.part.Type, ev.Type, tc.want)
			}
		})
	}
}
