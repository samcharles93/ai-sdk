package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
)

// TestExecuteToolCallsRunsConcurrently proves parallel execution actually
// happens rather than only asserting on results: two tools that each block
// until both have started can only both enter if the executor overlaps them.
// A sequential executor would deadlock here — the first tool would block
// forever waiting for a second that never starts.
func TestExecuteToolCallsRunsConcurrently(t *testing.T) {
	const n = 2
	entered := make(chan struct{}, n)
	release := make(chan struct{})

	set := ToolSet{}
	for i := range n {
		name := fmt.Sprintf("t%d", i)
		set[name] = &Tool{
			Execute: func(context.Context, string) (string, error) {
				entered <- struct{}{}
				<-release
				return "done", nil
			},
		}
	}
	calls := []ToolCall{
		{ToolCallID: "0", ToolName: "t0"},
		{ToolCallID: "1", ToolName: "t1"},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		executeToolCalls(context.Background(), calls, set, n)
	}()

	timeout := time.After(2 * time.Second)
	for range n {
		select {
		case <-entered:
		case <-timeout:
			t.Fatal("tools did not overlap: execution is not concurrent")
		}
	}
	close(release)
	<-done
}

// TestExecuteToolCallsParallelPreservesOrder checks results and messages
// stay in call order even when a later call finishes first.
func TestExecuteToolCallsParallelPreservesOrder(t *testing.T) {
	set := ToolSet{
		"slow": &Tool{Execute: func(context.Context, string) (string, error) {
			time.Sleep(50 * time.Millisecond)
			return "slow-out", nil
		}},
		"fast": &Tool{Execute: func(context.Context, string) (string, error) {
			return "fast-out", nil
		}},
	}
	calls := []ToolCall{
		{ToolCallID: "0", ToolName: "slow"},
		{ToolCallID: "1", ToolName: "fast"},
	}

	results, msgs := executeToolCalls(context.Background(), calls, set, 2)
	if len(results) != 2 || len(msgs) != 2 {
		t.Fatalf("len(results)=%d len(msgs)=%d, want 2/2", len(results), len(msgs))
	}
	if results[0].Output != "slow-out" || results[1].Output != "fast-out" {
		t.Fatalf("order not preserved: %+v", results)
	}
	if msgs[0].ToolCallID != "0" || msgs[1].ToolCallID != "1" {
		t.Fatalf("message order not preserved: %+v", msgs)
	}
}

// TestExecuteToolCallsParallelFailingCall checks a failing call records its
// error without affecting the others.
func TestExecuteToolCallsParallelFailingCall(t *testing.T) {
	set := ToolSet{
		"fail": &Tool{Execute: func(context.Context, string) (string, error) { return "", errors.New("boom") }},
		"ok":   &Tool{Execute: func(context.Context, string) (string, error) { return "good", nil }},
	}
	calls := []ToolCall{
		{ToolCallID: "0", ToolName: "fail"},
		{ToolCallID: "1", ToolName: "ok"},
	}

	results, _ := executeToolCalls(context.Background(), calls, set, 2)
	if results[0].Error != "boom" {
		t.Fatalf("results[0].Error = %q, want %q", results[0].Error, "boom")
	}
	if results[1].Output != "good" {
		t.Fatalf("results[1] = %+v, want the second call to have run", results[1])
	}
}

// TestExecuteToolCallsParallelToolCancelDoesNotTruncate mirrors the im2
// invariant under parallelism: a tool returning context.Canceled must not be
// mistaken for the caller's context ending, so the batch stays full.
func TestExecuteToolCallsParallelToolCancelDoesNotTruncate(t *testing.T) {
	set := ToolSet{
		"slow": &Tool{Execute: func(context.Context, string) (string, error) { return "", context.Canceled }},
		"fast": &Tool{Execute: func(context.Context, string) (string, error) { return "ok", nil }},
	}
	calls := []ToolCall{
		{ToolCallID: "0", ToolName: "slow"},
		{ToolCallID: "1", ToolName: "fast"},
	}

	results, msgs := executeToolCalls(context.Background(), calls, set, 2)
	if len(results) != 2 || len(msgs) != 2 {
		t.Fatalf("a tool's own Canceled must not truncate a live batch: %d/%d", len(results), len(msgs))
	}
	if results[1].Output != "ok" {
		t.Fatalf("results[1] = %+v, want the second call to have run", results[1])
	}
}

// TestExecuteToolCallsParallelCancelledContext checks a genuinely cancelled
// caller context spawns nothing and returns an empty batch.
func TestExecuteToolCallsParallelCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	set := ToolSet{
		"t": &Tool{Execute: func(context.Context, string) (string, error) { return "x", nil }},
	}
	calls := []ToolCall{{ToolCallID: "0", ToolName: "t"}, {ToolCallID: "1", ToolName: "t"}}

	results, msgs := executeToolCalls(ctx, calls, set, 2)
	if len(results) != 0 || len(msgs) != 0 {
		t.Fatalf("expected empty batch on cancelled ctx, got %d/%d", len(results), len(msgs))
	}
}

// TestExecuteToolCallsParallelDefaultSequential checks that zero (the zero
// value of MaxParallelToolCalls) routes to the sequential path.
func TestExecuteToolCallsParallelDefaultSequential(t *testing.T) {
	set := ToolSet{
		"a": &Tool{Execute: func(context.Context, string) (string, error) { return "a", nil }},
		"b": &Tool{Execute: func(context.Context, string) (string, error) { return "b", nil }},
	}
	calls := []ToolCall{{ToolCallID: "0", ToolName: "a"}, {ToolCallID: "1", ToolName: "b"}}

	results, msgs := executeToolCalls(context.Background(), calls, set, 0)
	if len(results) != 2 || len(msgs) != 2 {
		t.Fatalf("sequential path must still return full batches: %d/%d", len(results), len(msgs))
	}
}

// TestGenerateTextParallelToolCalls wires the option through the public
// entry point: a provider that requests two tools in one step, run with
// MaxParallelToolCalls=2, produces two results in call order.
func TestGenerateTextParallelToolCalls(t *testing.T) {
	p := &fakeProvider{
		chatScript: []chat.Response{
			{
				ToolCalls: []chat.ToolCall{
					{ID: "call_1", Name: "a", Arguments: `{}`},
					{ID: "call_2", Name: "b", Arguments: `{}`},
				},
				FinishReason: "tool_calls",
			},
			{Content: "done", FinishReason: "stop"},
		},
	}
	set := ToolSet{
		"a": NewTool("a", "", nil, func(context.Context, string) (string, error) { return "A", nil }),
		"b": NewTool("b", "", nil, func(context.Context, string) (string, error) { return "B", nil }),
	}

	got, err := GenerateText(context.Background(), p, GenerateOptions{
		Model:                "m",
		Prompt:               "x",
		Tools:                set,
		MaxSteps:             3,
		MaxParallelToolCalls: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ToolResults) != 2 {
		t.Fatalf("tool results = %d, want 2", len(got.ToolResults))
	}
	if got.ToolResults[0].Output != "A" || got.ToolResults[1].Output != "B" {
		t.Fatalf("results out of order: %+v", got.ToolResults)
	}
}
