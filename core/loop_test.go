package core

import (
	"context"
	"errors"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

func toolReturning(output string, err error) *Tool {
	return &Tool{
		Execute: func(context.Context, string) (string, error) {
			return output, err
		},
	}
}

func TestExecuteToolCalls(t *testing.T) {
	innerDeadline := errors.New("inner: deadline exceeded")

	tests := []struct {
		name        string
		calls       []ToolCall
		set         ToolSet
		ctxDone     bool
		wantResults int
		wantMsgs    int
		checkResult func(t *testing.T, results []ToolResult)
	}{
		{
			// A tool that owns its own inner timeout and returns
			// context.DeadlineExceeded (or wraps it) must not be mistaken
			// for the CALLER's context ending. Truncating the batch here
			// drops the tool results for every later call, and the next
			// provider request then names tool calls with no matching
			// tool response.
			name: "a tool's own DeadlineExceeded does not truncate a live batch",
			calls: []ToolCall{
				{ToolCallID: "1", ToolName: "slow"},
				{ToolCallID: "2", ToolName: "fast"},
			},
			set: ToolSet{
				"slow": toolReturning("", context.DeadlineExceeded),
				"fast": toolReturning("ok", nil),
			},
			wantResults: 2,
			wantMsgs:    2,
			checkResult: func(t *testing.T, results []ToolResult) {
				if results[0].Error == "" {
					t.Fatalf("results[0].Error is empty, want the tool's timeout recorded")
				}
				if results[1].Output != "ok" {
					t.Fatalf("results[1] = %+v, want the second call to have run", results[1])
				}
			},
		},
		{
			name: "a tool's own Canceled does not truncate a live batch",
			calls: []ToolCall{
				{ToolCallID: "1", ToolName: "slow"},
				{ToolCallID: "2", ToolName: "fast"},
			},
			set: ToolSet{
				"slow": toolReturning("", context.Canceled),
				"fast": toolReturning("ok", nil),
			},
			wantResults: 2,
			wantMsgs:    2,
			checkResult: func(t *testing.T, results []ToolResult) {
				if results[1].Output != "ok" {
					t.Fatalf("results[1] = %+v, want the second call to have run", results[1])
				}
			},
		},
		{
			// A wrapped context error (fmt.Errorf("%w: ...", context.DeadlineExceeded))
			// must be treated the same as the bare sentinel: errors.Is, not ==.
			name: "a wrapped context error from a tool does not truncate a live batch",
			calls: []ToolCall{
				{ToolCallID: "1", ToolName: "slow"},
				{ToolCallID: "2", ToolName: "fast"},
			},
			set: ToolSet{
				"slow": toolReturning("", errors.Join(innerDeadline, context.DeadlineExceeded)),
				"fast": toolReturning("ok", nil),
			},
			wantResults: 2,
			wantMsgs:    2,
			checkResult: func(t *testing.T, results []ToolResult) {
				if results[1].Output != "ok" {
					t.Fatalf("results[1] = %+v, want the second call to have run", results[1])
				}
			},
		},
		{
			// A genuinely cancelled CALLER context still truncates: the
			// remaining calls have nothing to run for.
			name: "a cancelled caller context truncates the batch",
			calls: []ToolCall{
				{ToolCallID: "1", ToolName: "slow"},
				{ToolCallID: "2", ToolName: "fast"},
			},
			set: ToolSet{
				"slow": toolReturning("", context.Canceled),
				"fast": toolReturning("ok", nil),
			},
			ctxDone:     true,
			wantResults: 0,
			wantMsgs:    0,
		},
		{
			name: "an ordinary tool error is recorded without truncating",
			calls: []ToolCall{
				{ToolCallID: "1", ToolName: "fails"},
				{ToolCallID: "2", ToolName: "fast"},
			},
			set: ToolSet{
				"fails": toolReturning("", errors.New("boom")),
				"fast":  toolReturning("ok", nil),
			},
			wantResults: 2,
			wantMsgs:    2,
			checkResult: func(t *testing.T, results []ToolResult) {
				if results[0].Error != "boom" {
					t.Fatalf("results[0].Error = %q, want %q", results[0].Error, "boom")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.ctxDone {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			results, msgs := executeToolCalls(ctx, tc.calls, tc.set, nil, 1)

			if len(results) != tc.wantResults {
				t.Fatalf("len(results) = %d, want %d (%+v)", len(results), tc.wantResults, results)
			}
			if len(msgs) != tc.wantMsgs {
				t.Fatalf("len(msgs) = %d, want %d", len(msgs), tc.wantMsgs)
			}
			for i, m := range msgs {
				if m.Role != chat.RoleTool {
					t.Fatalf("msgs[%d].Role = %v, want RoleTool", i, m.Role)
				}
			}
			if tc.checkResult != nil {
				tc.checkResult(t, results)
			}
		})
	}
}
