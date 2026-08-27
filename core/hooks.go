package core

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
)

// ToolHook is an interceptor for tool execution. Hooks compose in the
// order they were registered; each BeforeToolExecute sees the (possibly
// mutated) call from the previous hook, and the first non-nil Skip or
// deny short-circuits the chain. Implementations are expected to be
// safe for concurrent use across multiple tool calls in the same run.
//
// Hooks are the canonical interception seam for tool execution. They
// replace the hand-rolled "wrap tool.Execute in a closure" pattern
// used historically in agentloop and let callers express policy
// (permission gates, output sanitization, per-tool deadlines,
// telemetry) without reaching into core internals.
type ToolHook interface {
	// BeforeToolExecute runs before tool.Execute. A hook may:
	//   - mutate call.Input to replace the JSON-encoded arguments;
	//   - return a non-nil Skip to bypass the tool entirely (the Skip's
	//     Output/Error is fed back to the model in place of a real run);
	//   - return a non-nil error to deny the call (the error message is
	//     surfaced in-band to the model, same shape as a tool failure);
	//   - return (nil, nil) to proceed normally.
	//
	// When multiple Before hooks are registered, the chain stops at the
	// first non-nil Skip or non-nil error. Subsequent hooks are not
	// invoked and any mutations made to call by the short-circuiting
	// hook are observed only by the tool the chain would have run.
	BeforeToolExecute(ctx context.Context, call *ToolCall) (skip *Skip, deny error)

	// AfterToolExecute runs after tool.Execute returns (or after a
	// Before-hook Skip, or after a panic is contained). Hooks may
	// mutate result.Output and result.Error; the model-facing message
	// is built from the mutated result.
	//
	// After hooks always run, even when the tool itself errored or
	// was bypassed, so policy hooks can sanitise outputs and attach
	// metadata uniformly.
	AfterToolExecute(ctx context.Context, call ToolCall, result *ToolResult)
}

// Skip is the substitute result returned by a BeforeToolExecute hook to
// bypass tool execution entirely. Either Output or Error may be set;
// when Error is non-empty it surfaces as a tool error to the model,
// otherwise Output is fed back as a successful result.
type Skip struct {
	Output string
	Error  string
}

// ModelHook observes model-call lifecycle for liveness, latency, and
// telemetry. Hooks must treat the request and response as read-only. They are
// passed by value, but their maps, slices, and Parts may share backing storage
// with the live call. Both methods are called for every provider invocation;
// resp == nil with a non-nil err indicates a provider failure.
type ModelHook interface {
	// ModelCallStarted runs immediately before provider.Chat or
	// provider.ChatStream. Hooks may record the start time, attach
	// tracing attributes, etc.
	ModelCallStarted(ctx context.Context, req chat.Request)

	// ModelCallFinished runs after the provider returns (or errors).
	// latency is the wall-clock duration of the provider call.
	// usage is the per-call usage if available (nil for failed calls
	// or providers that surface usage only on the final chunk).
	ModelCallFinished(ctx context.Context, req chat.Request, resp *chat.Response, err error, latency time.Duration, usage *chat.Usage)
}

// ToolPanicError is the typed error produced when tool execution or a
// tool lifecycle hook panics. Phase identifies where in the lifecycle
// the panic was contained: "before" (BeforeToolExecute), "during"
// (tool.Execute), or "after" (AfterToolExecute). Value is the
// recovered panic payload; Stack is the goroutine's stack trace at
// the recover point.
type ToolPanicError struct {
	ToolName string
	Phase    string // "before" | "during" | "after"
	Value    any
	Stack    []byte
}

// Error renders the panic as a stable, model-friendly string. The stack trace
// is intentionally omitted from the message. Tool execution records this
// string in ToolResult.Error; code handling a ToolPanicError directly can
// inspect Stack before it is converted to that model-facing representation.
func (e *ToolPanicError) Error() string {
	return fmt.Sprintf("%s: tool %q panicked in %s phase: %v",
		ErrToolPanicked.Error(), e.ToolName, e.Phase, e.Value)
}

// Unwrap supports errors.Is(err, ErrToolPanicked) while preserving the panic
// details for callers handling ToolPanicError before it becomes ToolResult text.
func (e *ToolPanicError) Unwrap() error { return ErrToolPanicked }

// panicPhase is the canonical phase value used by runBeforeChain,
// safeToolExecute, and runAfterChain. Kept as named constants so tests
// can assert on them without typo-prone string matching.
const (
	panicPhaseBefore = "before"
	panicPhaseDuring = "during"
	panicPhaseAfter  = "after"
)

// toolHookFuncs adapts two function literals to the ToolHook
// interface. Provided as a convenience for one-off hooks that don't
// justify declaring a named type.
type toolHookFuncs struct {
	before func(ctx context.Context, call *ToolCall) (*Skip, error)
	after  func(ctx context.Context, call ToolCall, result *ToolResult)
}

func (h toolHookFuncs) BeforeToolExecute(ctx context.Context, call *ToolCall) (*Skip, error) {
	if h.before == nil {
		return nil, nil
	}
	return h.before(ctx, call)
}

func (h toolHookFuncs) AfterToolExecute(ctx context.Context, call ToolCall, result *ToolResult) {
	if h.after == nil {
		return
	}
	h.after(ctx, call, result)
}

// ToolHookFuncs returns a ToolHook whose BeforeToolExecute and
// AfterToolExecute methods delegate to the supplied function literals.
// Either may be nil; a nil callback is treated as a no-op for that
// phase. This is the ergonomic escape hatch for one-off hooks —
// callers that need richer state or named types should implement
// [ToolHook] directly.
func ToolHookFuncs(
	before func(ctx context.Context, call *ToolCall) (skip *Skip, deny error),
	after func(ctx context.Context, call ToolCall, result *ToolResult),
) ToolHook {
	return toolHookFuncs{before: before, after: after}
}

// modelHookFuncs adapts two function literals to the ModelHook
// interface. Convenience for one-off hooks.
type modelHookFuncs struct {
	started  func(ctx context.Context, req chat.Request)
	finished func(ctx context.Context, req chat.Request, resp *chat.Response, err error, latency time.Duration, usage *chat.Usage)
}

func (h modelHookFuncs) ModelCallStarted(ctx context.Context, req chat.Request) {
	if h.started == nil {
		return
	}
	h.started(ctx, req)
}

func (h modelHookFuncs) ModelCallFinished(ctx context.Context, req chat.Request, resp *chat.Response, err error, latency time.Duration, usage *chat.Usage) {
	if h.finished == nil {
		return
	}
	h.finished(ctx, req, resp, err, latency, usage)
}

// ModelHookFuncs returns a ModelHook whose ModelCallStarted and
// ModelCallFinished methods delegate to the supplied function
// literals. Either may be nil; a nil callback is treated as a no-op
// for that phase.
func ModelHookFuncs(
	started func(ctx context.Context, req chat.Request),
	finished func(ctx context.Context, req chat.Request, resp *chat.Response, err error, latency time.Duration, usage *chat.Usage),
) ModelHook {
	return modelHookFuncs{started: started, finished: finished}
}

// captureStack captures the goroutine's stack trace at the point of
// the recover. Used by runBeforeChain, safeToolExecute, and
// runAfterChain. Kept package-private so the stack format is owned by
// this file.
func captureStack() []byte {
	return debug.Stack()
}

// fireModelStarted invokes ModelCallStarted on every supplied hook. A
// panic in any hook is contained so a misbehaving observer cannot
// prevent the underlying model call.
func fireModelStarted(ctx context.Context, hooks []ModelHook, req chat.Request) {
	for _, h := range hooks {
		func() {
			defer func() { _ = recover() }()
			h.ModelCallStarted(ctx, req)
		}()
	}
}

// fireModelFinished invokes ModelCallFinished on every supplied hook,
// capturing latency for the hook. resp may be nil (provider failure);
// usage may be nil (no usage reported). A panic in any hook is
// contained.
func fireModelFinished(ctx context.Context, hooks []ModelHook, req chat.Request, resp *chat.Response, err error, latency time.Duration, usage *chat.Usage) {
	for _, h := range hooks {
		func() {
			defer func() { _ = recover() }()
			h.ModelCallFinished(ctx, req, resp, err, latency, usage)
		}()
	}
}
