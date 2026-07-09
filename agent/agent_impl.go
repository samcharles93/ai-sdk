package agent

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
)

// RunAgent orchestrates a multi-step agent run using the given provider,
// prompt, and tool set. It wraps [core.StreamText] and translates each
// [core.StreamPart] into a [StreamEvent], emitting them on the returned
// channel.
//
// [core.StreamText] handles the full tool loop internally — executing
// tools, feeding results back to the model, and respecting maxSteps
// for termination. RunAgent provides a simpler function-based API that
// avoids constructing an [Agent] struct.
//
// The returned channel is unbuffered. The agent goroutine respects ctx
// cancellation — when ctx is cancelled, an [EventAbort] is emitted and
// the channel closes.
func RunAgent(ctx context.Context, provider chat.Provider, prompt string, tools core.ToolSet, maxSteps int) (<-chan StreamEvent, error) {
	if provider == nil {
		return nil, fmt.Errorf("agent: no provider configured")
	}

	if maxSteps < 1 {
		maxSteps = 1
	}

	result, err := core.StreamText(ctx, provider, core.GenerateOptions{
		Prompt:   prompt,
		Tools:    tools,
		MaxSteps: maxSteps,
	})
	if err != nil {
		return nil, fmt.Errorf("agent: start stream: %w", err)
	}

	out := make(chan StreamEvent)
	go runAgentLoop(ctx, result.FullStream, out)
	return out, nil
}

// runAgentLoop is the goroutine that reads from core's FullStream channel
// and emits [StreamEvent] values on out until the stream is exhausted or
// ctx is cancelled.
func runAgentLoop(ctx context.Context, full <-chan core.StreamPart, out chan<- StreamEvent) {
	defer close(out)

	for {
		select {
		case <-ctx.Done():
			out <- StreamEvent{
				Type:  EventAbort,
				Error: fmt.Errorf("agent: context cancelled: %w", ctx.Err()),
			}
			return
		case part, ok := <-full:
			if !ok {
				return
			}
			ev := translate(part)
			select {
			case out <- ev:
			case <-ctx.Done():
				out <- StreamEvent{
					Type:  EventAbort,
					Error: fmt.Errorf("agent: context cancelled: %w", ctx.Err()),
				}
				return
			}
		}
	}
}
