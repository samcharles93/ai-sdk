package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/samcharles93/ai-sdk/chat"
)

// executeToolCalls runs each tool call from a model step against the
// provided [ToolSet], returning [ToolResult]s in the same order as the
// input calls and the [chat.Message]s that should be appended to the
// conversation before the next step.
//
// Errors from individual tool executions are recorded on the
// corresponding ToolResult (Error field) and surfaced as the message
// content fed back to the model — the loop does not abort on tool
// errors. Models are expected to react to error outputs the same way
// they react to ordinary tool outputs.
//
// A missing tool yields a ToolResult with Error set to a wrapped
// [ErrToolNotFound]; the conversation continues so the model can
// recover.
//
// maxParallel bounds how many calls run concurrently. One (or less)
// preserves the strictly sequential behaviour; a higher value runs
// independent calls in parallel, filling results[i]/msgs[i] by index so
// the returned slice order is unaffected by completion order.
//
// The batch is truncated early only when ctx itself has ended — checked
// directly, not inferred from a tool's returned error. A tool that owns an
// inner deadline, or that wraps a cancelled downstream call, can return
// context.Canceled or context.DeadlineExceeded while ctx is still live;
// treating that as "the caller gave up" would drop the remaining calls and
// their tool result messages, leaving the next provider request naming tool
// calls with no matching response.
// executeToolCalls runs each tool call from a model step against the
// provided [ToolSet], returning [ToolResult]s in the same order as the
// input calls and the [chat.Message]s that should be appended to the
// conversation before the next step.
//
// Errors from individual tool executions are recorded on the
// corresponding ToolResult (Error field) and surfaced as the message
// content fed back to the model — the loop does not abort on tool
// errors. Models are expected to react to error outputs the same way
// they react to ordinary tool outputs.
//
// A missing tool yields a ToolResult with Error set to a wrapped
// [ErrToolNotFound]; the conversation continues so the model can
// recover.
//
// hooks, when non-empty, run around every tool call (see [ToolHook]).
// They may inspect/replace input, bypass execution with a Skip, deny
// the call, or sanitise output. Panics in tool.Execute or in the hooks
// themselves are recovered and surfaced as a typed [ToolPanicError]
// in the corresponding ToolResult — they do not crash the run, even on
// the parallel path.
//
// maxParallel bounds how many calls run concurrently. One (or less)
// preserves the strictly sequential behaviour; a higher value runs
// independent calls in parallel, filling results[i]/msgs[i] by index so
// the returned slice order is unaffected by completion order.
//
// The batch is truncated early only when ctx itself has ended — checked
// directly, not inferred from a tool's returned error. A tool that owns an
// inner deadline, or that wraps a cancelled downstream call, can return
// context.Canceled or context.DeadlineExceeded while ctx is still live;
// treating that as "the caller gave up" would drop the remaining calls and
// their tool result messages, leaving the next provider request naming tool
// calls with no matching result.
func executeToolCalls(ctx context.Context, calls []ToolCall, set ToolSet, hooks []ToolHook, maxParallel int) ([]ToolResult, []chat.Message) {
	if len(calls) == 0 {
		return nil, nil
	}
	if maxParallel <= 1 {
		return executeToolCallsSequential(ctx, calls, set, hooks)
	}

	results := make([]ToolResult, len(calls))
	msgs := make([]chat.Message, len(calls))

	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	spawned := 0

spawnLoop:
	for i, call := range calls {
		// Check before acquiring the slot: when ctx is already cancelled,
		// both the send and the receive below are ready and select would
		// pick either, letting a call spawn after cancellation.
		if ctx.Err() != nil {
			break spawnLoop
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			// Cancellation stops further spawning; calls already in flight
			// finish (they observe ctx) and are included below.
			break spawnLoop
		}
		wg.Add(1)
		go func(i int, call ToolCall) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i], msgs[i] = runToolCall(ctx, call, set, hooks)
		}(i, call)
		spawned++
	}

	wg.Wait()
	return results[:spawned], msgs[:spawned]
}

// executeToolCallsSequential runs calls one at a time, checking ctx between
// calls so a cancelled caller truncates the batch at the first unstarted
// call.
// executeToolCallsSequential runs calls one at a time, checking ctx between
// calls so a cancelled caller truncates the batch at the first unstarted
// call. hooks are forwarded to runToolCall — see [executeToolCalls] for
// the hook chain semantics.
func executeToolCallsSequential(ctx context.Context, calls []ToolCall, set ToolSet, hooks []ToolHook) ([]ToolResult, []chat.Message) {
	results := make([]ToolResult, len(calls))
	msgs := make([]chat.Message, len(calls))
	for i, call := range calls {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return results[:i], msgs[:i]
		}
		results[i], msgs[i] = runToolCall(ctx, call, set, hooks)
	}
	return results, msgs
}

// runToolCall executes a single tool call and builds its ToolResult and the
// tool message fed back to the model. It is shared by the sequential and
// parallel paths so they behave identically per call.
// runToolCall executes a single tool call and builds its ToolResult and the
// tool message fed back to the model. It is shared by the sequential and
// parallel paths so they behave identically per call.
//
// When hooks are provided, runToolCall runs the BeforeToolExecute chain
// (with panic containment) before the tool, the tool itself (with panic
// containment), then the AfterToolExecute chain (with panic containment).
// A non-nil Skip from the Before chain bypasses the tool; a non-nil
// deny error surfaces in-band to the model. After hooks always run so
// they can sanitise or attach metadata uniformly.
//
// Panics in any of these phases are recovered and surfaced as a
// [ToolPanicError] recorded in result.Error — the panic does not
// propagate out of runToolCall, even on the parallel goroutine path.
func runToolCall(ctx context.Context, call ToolCall, set ToolSet, hooks []ToolHook) (ToolResult, chat.Message) {
	res := ToolResult{ToolCallID: call.ToolCallID, ToolName: call.ToolName}

	// Run the BeforeToolExecute chain. A panic in any Before hook is
	// captured and surfaces here as the deny error so the tool is not
	// executed.
	skip, deny := runBeforeChain(ctx, hooks, &call)
	switch {
	case deny != nil:
		res.Error = deny.Error()
	case skip != nil:
		res.Output = skip.Output
		res.Error = skip.Error
	default:
		tool, ok := set[call.ToolName]
		switch {
		case !ok || tool == nil:
			res.Error = fmt.Errorf("%w: %q", ErrToolNotFound, call.ToolName).Error()
		case tool.Execute == nil:
			res.Error = fmt.Errorf("%w: tool %q has no Execute function", ErrToolExecutionFailed, call.ToolName).Error()
		default:
			out, err := safeToolExecute(ctx, call, tool)
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Output = out
			}
		}
	}

	// After hooks always run, including after a Skip, a deny, or a
	// recovered panic — they may sanitise or attach metadata uniformly.
	// A panic in any After hook is recovered and overrides res.Error so
	// the policy failure surfaces in-band to the model.
	runAfterChain(ctx, hooks, call, &res)

	// Feed the tool's output (or error string) back to the model as the
	// message content. Per-provider mapping (Gemini's role:"user" quirk,
	// Ollama's positional matching) lives inside the provider.
	content := res.Output
	if res.Error != "" {
		content = res.Error
	}
	msg := chat.Message{
		Role:       chat.RoleTool,
		Content:    content,
		Name:       call.ToolName,
		ToolCallID: call.ToolCallID,
	}
	return res, msg
}

// runBeforeChain iterates hooks, calling BeforeToolExecute on each.
// The first hook to return a non-nil Skip or non-nil deny short-circuits
// the chain. A panic in any Before hook is recovered and surfaces as a
// deny error wrapping a ToolPanicError so the tool is not invoked.
func runBeforeChain(ctx context.Context, hooks []ToolHook, call *ToolCall) (*Skip, error) {
	for _, h := range hooks {
		var (
			skip *Skip
			err  error
		)
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = &ToolPanicError{
						ToolName: call.ToolName,
						Phase:    panicPhaseBefore,
						Value:    r,
						Stack:    captureStack(),
					}
				}
			}()
			skip, err = h.BeforeToolExecute(ctx, call)
		}()
		if err != nil || skip != nil {
			return skip, err
		}
	}
	return nil, nil
}

// runAfterChain iterates hooks, calling AfterToolExecute on each. A
// panic in any After hook is recovered; the resulting ToolPanicError
// overrides res.Error so the policy failure surfaces in-band to the
// model. After hooks cannot short-circuit each other — every hook
// runs, in order.
func runAfterChain(ctx context.Context, hooks []ToolHook, call ToolCall, res *ToolResult) {
	for _, h := range hooks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					res.Error = (&ToolPanicError{
						ToolName: call.ToolName,
						Phase:    panicPhaseAfter,
						Value:    r,
						Stack:    captureStack(),
					}).Error()
				}
			}()
			h.AfterToolExecute(ctx, call, res)
		}()
	}
}

// safeToolExecute invokes tool.Execute under a panic-recover so an
// panicking tool cannot crash the run, including on the parallel
// goroutine path. A recovered panic becomes a ToolPanicError with
// Phase == "during"; the tool's panic value and stack are preserved
// on the returned error for the caller's observability.
func safeToolExecute(ctx context.Context, call ToolCall, tool *Tool) (string, error) {
	var (
		out string
		err error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = &ToolPanicError{
					ToolName: call.ToolName,
					Phase:    panicPhaseDuring,
					Value:    r,
					Stack:    captureStack(),
				}
			}
		}()
		out, err = tool.Execute(ctx, call.Input)
	}()
	return out, err
}

// assistantMessageFromResponse builds the [chat.Message] to append to the
// conversation representing the assistant turn that just completed. It
// preserves content, multimodal Parts, and any tool calls so that
// subsequent provider calls see the complete history — this is essential
// for providers that require opaque replay tokens (Anthropic thinking
// signatures, OpenAI o1 reasoning) to be sent back unchanged.
func assistantMessageFromResponse(resp chat.Response) chat.Message {
	return chat.Message{
		Role:            chat.RoleAssistant,
		Content:         resp.Content,
		Parts:           resp.Parts,
		ToolCalls:       resp.ToolCalls,
		ProviderOptions: resp.ProviderMetadata,
	}
}

// assistantMessageFromCalls builds an assistant [chat.Message] for the
// streaming path, where the assembled text and the assembled tool calls
// are computed separately rather than coming from a [chat.Response].
// reasoning, when non-empty, is preserved as a leading [chat.ReasoningPart]
// so providers like Anthropic can replay thinking blocks on subsequent
// turns.
func assistantMessageFromCalls(
	text string,
	reasoning string,
	calls []chat.ToolCall,
	providerMetadata map[string]any,
) chat.Message {
	m := chat.Message{
		Role:            chat.RoleAssistant,
		Content:         text,
		ToolCalls:       calls,
		ProviderOptions: providerMetadata,
	}
	if reasoning != "" {
		// Build canonical Parts: reasoning first, then text.
		parts := make(chat.Parts, 0, 2)
		parts = append(parts, chat.ReasoningPart{Text: reasoning})
		if text != "" {
			parts = append(parts, chat.TextPart{Text: text})
		}
		m.Parts = parts
	}
	return m
}
