package agent

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
)

// EventType identifies the kind of a [StreamEvent].
type EventType string

// StreamEvent event type constants.
const (
	EventTextDelta      EventType = "text-delta"
	EventReasoningDelta EventType = "reasoning-delta"
	EventToolCall       EventType = "tool-call"
	EventToolResult     EventType = "tool-result"
	EventStartStep      EventType = "step-start"
	EventFinishStep     EventType = "finish-step"
	EventFinish         EventType = "finish"
	EventError          EventType = "error"
	EventAbort          EventType = "abort"
)

// StreamEvent is a single event emitted during agent execution. Each
// event has a [Type] and at most one payload field populated — the
// payload depends on the [Type].
type StreamEvent struct {
	Type           EventType         `json:"type"`
	TextDelta      string            `json:"text_delta,omitempty"`
	ReasoningDelta string            `json:"reasoning_delta,omitempty"`
	ToolCall       *core.ToolCall    `json:"tool_call,omitempty"`
	ToolResult     *core.ToolResult  `json:"tool_result,omitempty"`
	StepResult     *core.StepResult  `json:"step_result,omitempty"`
	Error          error             `json:"-"`
	FinishReason   core.FinishReason `json:"finish_reason,omitempty"`
	TotalUsage     *chat.Usage       `json:"total_usage,omitempty"`
}

// Agent orchestrates multi-step reasoning and tool execution by wrapping
// [core.StreamText]. Callers configure the agent and call [Agent.Run] to
// receive a channel of [StreamEvent] values.
type Agent struct {
	Provider    chat.Provider `json:"-"`
	Model       string        `json:"model,omitempty"`
	System      string        `json:"system,omitempty"`
	Tools       core.ToolSet  `json:"-"`
	MaxSteps    int           `json:"max_steps,omitempty"`
	Temperature float32       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

// Run starts the agent with the given prompt and returns a channel of
// stream events. The agent goroutine drives [core.StreamText], translates
// [core.StreamPart] events into [StreamEvent] values, and closes the
// channel when the run completes.
//
// The returned channel is unbuffered. The agent goroutine respects ctx
// cancellation — when ctx is cancelled, an [EventAbort] is emitted and
// the channel closes.
func (a *Agent) Run(ctx context.Context, prompt string) (<-chan StreamEvent, error) {
	if a.Provider == nil {
		return nil, fmt.Errorf("agent: no provider configured")
	}

	maxSteps := max(a.MaxSteps, 1)

	result, err := core.StreamText(ctx, a.Provider, core.GenerateOptions{
		Model:       a.Model,
		System:      a.System,
		Prompt:      prompt,
		Tools:       a.Tools,
		MaxSteps:    maxSteps,
		Temperature: a.Temperature,
		MaxTokens:   a.MaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("agent: start stream: %w", err)
	}

	out := make(chan StreamEvent)
	go a.run(ctx, result.FullStream, out)
	return out, nil
}

// run is the event-translation goroutine. It reads from core's
// FullStream channel and emits [StreamEvent] values on out until the
// stream is exhausted or ctx is cancelled.
func (a *Agent) run(ctx context.Context, full <-chan core.StreamPart, out chan<- StreamEvent) {
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

// translate maps a single [core.StreamPart] to a [StreamEvent].
func translate(p core.StreamPart) StreamEvent {
	switch p.Type {
	case core.StreamPartTextDelta:
		return StreamEvent{Type: EventTextDelta, TextDelta: p.TextDelta}
	case core.StreamPartReasoningDelta:
		return StreamEvent{Type: EventReasoningDelta, ReasoningDelta: p.ReasoningDelta}
	case core.StreamPartToolCall:
		return StreamEvent{Type: EventToolCall, ToolCall: p.ToolCall}
	case core.StreamPartToolResult:
		return StreamEvent{Type: EventToolResult, ToolResult: p.ToolResult}
	case core.StreamPartStartStep:
		return StreamEvent{Type: EventStartStep}
	case core.StreamPartFinishStep:
		return StreamEvent{Type: EventFinishStep, StepResult: p.StepResult}
	case core.StreamPartFinish:
		return StreamEvent{
			Type:         EventFinish,
			FinishReason: p.FinishReason,
			TotalUsage:   p.TotalUsage,
		}
	case core.StreamPartError:
		return StreamEvent{Type: EventError, Error: p.Error}
	case core.StreamPartAbort:
		return StreamEvent{Type: EventAbort, Error: p.Error}
	default:
		return StreamEvent{Type: EventFinish}
	}
}
