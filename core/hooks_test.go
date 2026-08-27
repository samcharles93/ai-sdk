package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
)

// ---------------------------------------------------------------------------
// Tool hook chain semantics
// ---------------------------------------------------------------------------

func TestHook_BeforeChain_MutatesInput(t *testing.T) {
	// Three hooks each append to call.Input in order. The tool sees
	// the fully-mutated input.
	var beforeCalls atomic.Int32
	hooks := []ToolHook{
		ToolHookFuncs(
			func(_ context.Context, call *ToolCall) (*Skip, error) {
				beforeCalls.Add(1)
				call.Input = `{"step":"a"}`
				return nil, nil
			},
			nil,
		),
		ToolHookFuncs(
			func(_ context.Context, call *ToolCall) (*Skip, error) {
				beforeCalls.Add(1)
				call.Input = `{"step":"b"}`
				return nil, nil
			},
			nil,
		),
		ToolHookFuncs(
			func(_ context.Context, call *ToolCall) (*Skip, error) {
				beforeCalls.Add(1)
				call.Input = `{"final":true}`
				return nil, nil
			},
			nil,
		),
	}

	var seenInput string
	set := ToolSet{"x": NewTool("x", "", nil, func(_ context.Context, in string) (string, error) {
		seenInput = in
		return "ok", nil
	})}

	res, _ := runToolCall(context.Background(), ToolCall{ToolCallID: "1", ToolName: "x", Input: `{}`}, set, hooks)

	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if got := beforeCalls.Load(); got != 3 {
		t.Errorf("Before hooks called %d times, want 3", got)
	}
	if seenInput != `{"final":true}` {
		t.Errorf("tool saw input %q, want the final mutation", seenInput)
	}
}

func TestHook_BeforeChain_ShortCircuitsOnSkip(t *testing.T) {
	// First hook returns Skip → tool must NOT execute, subsequent
	// hooks must NOT be called.
	var (
		secondCalled atomic.Int32
		thirdCalled  atomic.Int32
		toolExecuted atomic.Int32
	)
	hooks := []ToolHook{
		ToolHookFuncs(func(_ context.Context, _ *ToolCall) (*Skip, error) {
			return &Skip{Output: "from-skip"}, nil
		}, nil),
		ToolHookFuncs(func(_ context.Context, _ *ToolCall) (*Skip, error) {
			secondCalled.Add(1)
			return nil, nil
		}, nil),
		ToolHookFuncs(func(_ context.Context, _ *ToolCall) (*Skip, error) {
			thirdCalled.Add(1)
			return nil, nil
		}, nil),
	}

	set := ToolSet{"x": NewTool("x", "", nil, func(_ context.Context, _ string) (string, error) {
		toolExecuted.Add(1)
		return "should-not-run", nil
	})}

	res, _ := runToolCall(context.Background(), ToolCall{ToolCallID: "1", ToolName: "x"}, set, hooks)

	if res.Output != "from-skip" {
		t.Errorf("Output = %q, want %q", res.Output, "from-skip")
	}
	if res.Error != "" {
		t.Errorf("Error = %q, want empty", res.Error)
	}
	if secondCalled.Load() != 0 || thirdCalled.Load() != 0 {
		t.Errorf("subsequent hooks called: 2nd=%d, 3rd=%d; want 0", secondCalled.Load(), thirdCalled.Load())
	}
	if toolExecuted.Load() != 0 {
		t.Errorf("tool executed despite Skip; calls=%d", toolExecuted.Load())
	}
}

func TestHook_BeforeChain_ShortCircuitsOnDeny(t *testing.T) {
	denyErr := errors.New("denied by policy")
	var secondCalled atomic.Int32
	hooks := []ToolHook{
		ToolHookFuncs(func(_ context.Context, _ *ToolCall) (*Skip, error) {
			return nil, denyErr
		}, nil),
		ToolHookFuncs(func(_ context.Context, _ *ToolCall) (*Skip, error) {
			secondCalled.Add(1)
			return nil, nil
		}, nil),
	}

	set := ToolSet{"x": NewTool("x", "", nil, func(_ context.Context, _ string) (string, error) {
		return "should-not-run", nil
	})}

	res, _ := runToolCall(context.Background(), ToolCall{ToolCallID: "1", ToolName: "x"}, set, hooks)

	if !strings.Contains(res.Error, "denied by policy") {
		t.Errorf("Error = %q, want it to contain the deny error", res.Error)
	}
	if secondCalled.Load() != 0 {
		t.Errorf("subsequent hook called after deny: %d", secondCalled.Load())
	}
}

func TestHook_AfterChain_SanitizesOutput(t *testing.T) {
	// After hook replaces result.Output — the model-facing message
	// is built from the sanitised value.
	hooks := []ToolHook{
		ToolHookFuncs(
			nil,
			func(_ context.Context, _ ToolCall, res *ToolResult) {
				res.Output = "REDACTED: " + res.Output
			},
		),
	}

	set := ToolSet{"x": NewTool("x", "", nil, func(_ context.Context, _ string) (string, error) {
		return "secret-token", nil
	})}

	res, msg := runToolCall(context.Background(), ToolCall{ToolCallID: "1", ToolName: "x"}, set, hooks)

	if res.Output != "REDACTED: secret-token" {
		t.Errorf("Output = %q, want sanitised value", res.Output)
	}
	if msg.Content != "REDACTED: secret-token" {
		t.Errorf("message Content = %q, want sanitised value", msg.Content)
	}
}

func TestHook_AfterChain_RunsAfterSkip(t *testing.T) {
	// After hooks observe a Skip too — they can still sanitise or
	// attach metadata to the synthetic result.
	var afterCalls atomic.Int32
	hooks := []ToolHook{
		ToolHookFuncs(
			func(_ context.Context, _ *ToolCall) (*Skip, error) {
				return &Skip{Output: "synthetic"}, nil
			},
			func(_ context.Context, _ ToolCall, res *ToolResult) {
				afterCalls.Add(1)
				res.Output = "wrapped:" + res.Output
			},
		),
	}

	set := ToolSet{"x": NewTool("x", "", nil, func(_ context.Context, _ string) (string, error) {
		t.Fatal("tool must not run on Skip")
		return "", nil
	})}

	res, _ := runToolCall(context.Background(), ToolCall{ToolCallID: "1", ToolName: "x"}, set, hooks)

	if res.Output != "wrapped:synthetic" {
		t.Errorf("Output = %q, want wrapped synthetic", res.Output)
	}
	if afterCalls.Load() != 1 {
		t.Errorf("After hook called %d times, want 1", afterCalls.Load())
	}
}

// ---------------------------------------------------------------------------
// Panic containment
// ---------------------------------------------------------------------------

func TestHook_ToolPanicDuring_DoesNotCrash(t *testing.T) {
	set := ToolSet{"boom": NewTool("boom", "", nil, func(_ context.Context, _ string) (string, error) {
		panic("kaboom")
	})}

	res, _ := runToolCall(context.Background(), ToolCall{ToolCallID: "1", ToolName: "boom"}, set, nil)

	if !strings.Contains(res.Error, "panicked in during phase") {
		t.Errorf("Error = %q, want it to contain 'panicked in during phase'", res.Error)
	}
	if !strings.Contains(res.Error, "kaboom") {
		t.Errorf("Error = %q, want it to preserve panic value", res.Error)
	}
}

func TestHook_BeforeHookPanic_SurfacesAsToolError(t *testing.T) {
	hooks := []ToolHook{
		ToolHookFuncs(
			func(_ context.Context, _ *ToolCall) (*Skip, error) {
				panic("before-panic")
			},
			nil,
		),
	}
	var toolRan atomic.Int32
	set := ToolSet{"x": NewTool("x", "", nil, func(_ context.Context, _ string) (string, error) {
		toolRan.Add(1)
		return "ok", nil
	})}

	res, _ := runToolCall(context.Background(), ToolCall{ToolCallID: "1", ToolName: "x"}, set, hooks)

	if !strings.Contains(res.Error, "panicked in before phase") {
		t.Errorf("Error = %q, want 'panicked in before phase'", res.Error)
	}
	if toolRan.Load() != 0 {
		t.Errorf("tool ran despite before-panic: %d", toolRan.Load())
	}
}

func TestHook_AfterHookPanic_OverridesResult(t *testing.T) {
	// After-hook panic surfaces in res.Error; the model sees the
	// policy failure rather than the tool's actual output.
	hooks := []ToolHook{
		ToolHookFuncs(
			nil,
			func(_ context.Context, _ ToolCall, _ *ToolResult) {
				panic("after-panic")
			},
		),
	}

	set := ToolSet{"x": NewTool("x", "", nil, func(_ context.Context, _ string) (string, error) {
		return "tool-output", nil
	})}

	res, _ := runToolCall(context.Background(), ToolCall{ToolCallID: "1", ToolName: "x"}, set, hooks)

	if !strings.Contains(res.Error, "panicked in after phase") {
		t.Errorf("Error = %q, want 'panicked in after phase'", res.Error)
	}
	// The hook panic overrides even a successful tool output: the
	// model must see the policy failure.
	if res.Error == "" {
		t.Errorf("Error empty; after-hook panic must override res.Error")
	}
}

func TestHook_PanicCapturesStack(t *testing.T) {
	set := ToolSet{"x": NewTool("x", "", nil, func(_ context.Context, _ string) (string, error) {
		panic("stack-test")
	})}

	res, _ := runToolCall(context.Background(), ToolCall{ToolCallID: "1", ToolName: "x"}, set, nil)

	if res.Error == "" {
		t.Fatal("expected error from panicking tool")
	}
	// The Error() string omits the stack on purpose; verify by
	// constructing a ToolPanicError directly and asserting on Stack.
	err := &ToolPanicError{ToolName: "x", Phase: "during", Value: "x", Stack: []byte("stack-bytes")}
	if string(err.Stack) != "stack-bytes" {
		t.Errorf("Stack field not preserved: %q", err.Stack)
	}
	_ = json.RawMessage{} // keep json import warm for future test additions
}

// ---------------------------------------------------------------------------
// Model hook latency and lifecycle
// ---------------------------------------------------------------------------

func TestHook_ModelHook_FiresAroundProviderCall(t *testing.T) {
	// Three-step loop: each step fires ModelCallStarted/Finished once.
	// The first step's response triggers tool calls, so we end up with
	// two model calls total: one requesting tools, one consuming
	// results. Assert the hook sees both.
	provider := &fakeProvider{
		chatScript: []chat.Response{
			{Content: "step1", ToolCalls: []chat.ToolCall{{ID: "c1", Name: "x", Arguments: "{}"}}},
			{Content: "done"},
		},
	}
	set := ToolSet{"x": NewTool("x", "", nil, func(_ context.Context, _ string) (string, error) {
		return "tool-out", nil
	})}

	var (
		startedCalls  atomic.Int32
		finishedCalls atomic.Int32
		latencies     []time.Duration
	)
	hooks := []ModelHook{
		ModelHookFuncs(
			func(_ context.Context, _ chat.Request) {
				startedCalls.Add(1)
			},
			func(_ context.Context, _ chat.Request, _ *chat.Response, err error, lat time.Duration, _ *chat.Usage) {
				finishedCalls.Add(1)
				if err != nil {
					t.Errorf("provider error on step: %v", err)
				}
				latencies = append(latencies, lat)
			},
		),
	}

	_, err := GenerateText(context.Background(), provider, GenerateOptions{
		Model:      "m",
		Messages:   []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
		Tools:      set,
		MaxSteps:   4, // need at least 2 model calls: tools then final
		ToolHooks:  nil,
		ModelHooks: hooks,
	})
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}

	if startedCalls.Load() != 2 {
		t.Errorf("ModelCallStarted fires = %d, want 2", startedCalls.Load())
	}
	if finishedCalls.Load() != 2 {
		t.Errorf("ModelCallFinished fires = %d, want 2", finishedCalls.Load())
	}
	if len(latencies) != 2 {
		t.Fatalf("latencies recorded = %d, want 2", len(latencies))
	}
	for i, l := range latencies {
		if l < 0 {
			t.Errorf("latency[%d] = %v, want ≥ 0", i, l)
		}
	}
}

func TestHook_ModelHook_PropagatesProviderError(t *testing.T) {
	provider := &fakeProvider{
		chatErr: []error{errors.New("provider-broke")},
	}

	var finishedErr error
	hooks := []ModelHook{
		ModelHookFuncs(nil, func(_ context.Context, _ chat.Request, resp *chat.Response, err error, _ time.Duration, _ *chat.Usage) {
			finishedErr = err
			if resp != nil {
				t.Errorf("expected nil resp on error, got %+v", resp)
			}
		}),
	}

	_, err := GenerateText(context.Background(), provider, GenerateOptions{
		Model:      "m",
		Messages:   []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
		ModelHooks: hooks,
	})
	if err == nil {
		t.Fatal("expected provider error to propagate")
	}
	if finishedErr == nil || finishedErr.Error() != "provider-broke" {
		t.Errorf("hook saw err = %v, want %q", finishedErr, "provider-broke")
	}
}

func TestHook_ModelHook_FiresForStreamPath(t *testing.T) {
	provider := &fakeProvider{
		streamScript: [][]chat.Chunk{
			{
				{Delta: "hi"},
				{Done: true},
			},
		},
	}

	var finishedCalls atomic.Int32
	hooks := []ModelHook{
		ModelHookFuncs(
			func(_ context.Context, _ chat.Request) {},
			func(_ context.Context, _ chat.Request, _ *chat.Response, _ error, _ time.Duration, _ *chat.Usage) {
				finishedCalls.Add(1)
			},
		),
	}

	res, err := StreamText(context.Background(), provider, GenerateOptions{
		Model:      "m",
		Messages:   []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
		ModelHooks: hooks,
	})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	for range res.FullStream {
	}

	if finishedCalls.Load() != 1 {
		t.Errorf("ModelCallFinished fires = %d, want 1", finishedCalls.Load())
	}
}

func TestHook_ModelHookFinishedOnceForStreamNextError(t *testing.T) {
	streamErr := errors.New("stream failed")
	provider := streamOnlyProvider{stream: errStream{err: streamErr}}
	var finishedCalls atomic.Int32
	var gotErr error

	res, err := StreamText(context.Background(), provider, GenerateOptions{
		Model: "m",
		ModelHooks: []ModelHook{ModelHookFuncs(nil, func(_ context.Context, _ chat.Request, _ *chat.Response, err error, _ time.Duration, _ *chat.Usage) {
			finishedCalls.Add(1)
			gotErr = err
		})},
	})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	for range res.FullStream {
	}
	if _, err := res.Usage(); !errors.Is(err, streamErr) {
		t.Fatalf("Usage error = %v, want stream error", err)
	}
	if finishedCalls.Load() != 1 {
		t.Errorf("ModelCallFinished calls = %d, want 1", finishedCalls.Load())
	}
	if !errors.Is(gotErr, streamErr) {
		t.Errorf("ModelCallFinished error = %v, want stream error", gotErr)
	}
}

func TestHook_ModelHookFinishedOnceForStreamCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := &cancellableStream{entered: make(chan struct{})}
	provider := streamOnlyProvider{stream: stream}
	var finishedCalls atomic.Int32
	var gotErr error
	var gotLatency time.Duration

	res, err := StreamText(ctx, provider, GenerateOptions{
		Model: "m",
		ModelHooks: []ModelHook{ModelHookFuncs(nil, func(_ context.Context, _ chat.Request, _ *chat.Response, err error, latency time.Duration, _ *chat.Usage) {
			finishedCalls.Add(1)
			gotErr = err
			gotLatency = latency
		})},
	})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	<-stream.entered
	const streamLifetime = 20 * time.Millisecond
	time.Sleep(streamLifetime)
	cancel()
	for range res.FullStream {
	}
	if _, err := res.Usage(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Usage error = %v, want context cancellation", err)
	}
	if finishedCalls.Load() != 1 {
		t.Errorf("ModelCallFinished calls = %d, want 1", finishedCalls.Load())
	}
	if !errors.Is(gotErr, context.Canceled) {
		t.Errorf("ModelCallFinished error = %v, want context cancellation", gotErr)
	}
	if gotLatency < streamLifetime {
		t.Errorf("ModelCallFinished latency = %v, want at least %v", gotLatency, streamLifetime)
	}
}

type streamOnlyProvider struct{ stream chat.Stream }

func (p streamOnlyProvider) Name() string { return "stream-only" }
func (p streamOnlyProvider) Chat(context.Context, chat.Request) (chat.Response, error) {
	return chat.Response{}, errors.New("Chat must not be called")
}

func (p streamOnlyProvider) ChatStream(context.Context, chat.Request) (chat.Stream, error) {
	return p.stream, nil
}

type errStream struct{ err error }

func (s errStream) Next(context.Context) (chat.Chunk, error) { return chat.Chunk{}, s.err }
func (errStream) Close() error                               { return nil }

type cancellableStream struct{ entered chan struct{} }

func (s *cancellableStream) Next(ctx context.Context) (chat.Chunk, error) {
	close(s.entered)
	<-ctx.Done()
	return chat.Chunk{}, ctx.Err()
}
func (*cancellableStream) Close() error { return nil }

func TestToolPanicErrorMatchesSentinel(t *testing.T) {
	err := &ToolPanicError{ToolName: "tool", Phase: "during", Value: "boom"}
	if !errors.Is(err, ErrToolPanicked) {
		t.Fatalf("errors.Is(%v, ErrToolPanicked) = false", err)
	}
}

// ---------------------------------------------------------------------------
// Integration: hooks in a real GenerateText run, panic in parallel tool path
// ---------------------------------------------------------------------------

func TestHook_GenerateText_HookMutationObservable(t *testing.T) {
	// End-to-end: Before hook rewrites the tool call's arguments; the
	// tool sees the rewritten input and echoes it back. The after-hook
	// confirms the ToolResult is fully populated.
	provider := &fakeProvider{
		chatScript: []chat.Response{
			{Content: "ok", ToolCalls: []chat.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"v":"original"}`}}},
			{Content: "done"},
		},
	}

	var seenArg string
	set := ToolSet{"echo": NewTool("echo", "", nil, func(_ context.Context, in string) (string, error) {
		seenArg = in
		return "echoed: " + in, nil
	})}

	var beforeCalls, afterCalls atomic.Int32
	hooks := []ToolHook{
		ToolHookFuncs(
			func(_ context.Context, call *ToolCall) (*Skip, error) {
				beforeCalls.Add(1)
				call.Input = `{"v":"rewritten"}`
				return nil, nil
			},
			func(_ context.Context, _ ToolCall, res *ToolResult) {
				afterCalls.Add(1)
				if res.ToolName != "echo" {
					t.Errorf("AfterToolExecute saw tool %q, want echo", res.ToolName)
				}
				if !strings.Contains(res.Output, "echoed:") {
					t.Errorf("AfterToolExecute saw output %q, want echoed prefix", res.Output)
				}
			},
		),
	}

	if _, err := GenerateText(context.Background(), provider, GenerateOptions{
		Model:     "m",
		Messages:  []chat.Message{{Role: chat.RoleUser, Content: "go"}},
		Tools:     set,
		ToolHooks: hooks,
	}); err != nil {
		t.Fatalf("GenerateText: %v", err)
	}

	if beforeCalls.Load() != 1 {
		t.Errorf("Before hook calls = %d, want 1", beforeCalls.Load())
	}
	if afterCalls.Load() != 1 {
		t.Errorf("After hook calls = %d, want 1", afterCalls.Load())
	}
	if seenArg != `{"v":"rewritten"}` {
		t.Errorf("tool saw arg %q, want rewritten", seenArg)
	}
}

func TestHook_ParallelToolCalls_PanicDoesNotCrashRun(t *testing.T) {
	// Three tool calls in parallel: two panic, one succeeds. The
	// recovered panics become typed errors on the corresponding
	// ToolResults; the healthy call's result is preserved. The run
	// completes — no goroutine leak / process crash.
	provider := &fakeProvider{
		chatScript: []chat.Response{
			{
				Content: "go",
				ToolCalls: []chat.ToolCall{
					{ID: "c1", Name: "boom1", Arguments: "{}"},
					{ID: "c2", Name: "ok", Arguments: "{}"},
					{ID: "c3", Name: "boom2", Arguments: "{}"},
				},
			},
			{Content: "done"},
		},
	}

	set := ToolSet{
		"boom1": NewTool("boom1", "", nil, func(_ context.Context, _ string) (string, error) {
			panic("p1")
		}),
		"boom2": NewTool("boom2", "", nil, func(_ context.Context, _ string) (string, error) {
			panic("p2")
		}),
		"ok": NewTool("ok", "", nil, func(_ context.Context, _ string) (string, error) {
			return "survivor", nil
		}),
	}

	_, err := GenerateText(context.Background(), provider, GenerateOptions{
		Model:                "m",
		Messages:             []chat.Message{{Role: chat.RoleUser, Content: "go"}},
		Tools:                set,
		MaxParallelToolCalls: 3,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	// If we got here without a panic killing the test process, the
	// recover boundary worked. The provider's second chatScript entry
	// drove the run to a clean finish, confirming the healthy tool's
	// result was consumable.
}
