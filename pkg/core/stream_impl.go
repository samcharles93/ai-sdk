package core

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

// streamBufferSize sizes the FullStream and TextStream channels.
// Generous enough to absorb burst chunks from a fast provider while
// keeping memory bounded.
const streamBufferSize = 64

// StreamText performs a streaming text generation with optional tool
// calling. It returns a [StreamResult] that exposes channels for
// incremental text, tool calls, and step boundaries.
//
// The implementation runs a producer goroutine that drives the
// underlying [chat.Provider.ChatStream], assembles tool-call deltas
// across chunks, executes any requested tools via [GenerateOptions.Tools],
// and feeds tool results back to the model until [GenerateOptions.StopWhen]
// (or the default StepCountIs(MaxSteps)) terminates the loop.
//
// Channel contract:
//
//   - FullStream is the authoritative event stream and MUST be drained
//     by the caller until it is closed. Its writes are synchronous —
//     a slow consumer applies natural backpressure to the producer.
//   - TextStream is a convenience view emitting only text deltas. Its
//     writes are best-effort: if the consumer is not draining
//     TextStream the SDK drops deltas rather than stalling the
//     producer. Callers that need every text delta should consume
//     [StreamPartTextDelta] events from FullStream.
//   - Usage and FinishReason are futures that block until the producer
//     completes; they return any terminal error from the run.
//
// This shape integrates directly with goroutine + channel transports
// such as DirectTransport in pkg/ui/chat — adapters can range over
// FullStream and translate [StreamPart]s into their wire vocabulary.
//
// Cancellation: the producer respects ctx — when ctx is cancelled an
// abort event is emitted and the channels close. Callers should still
// drain FullStream until close to release the producer.
func StreamText(ctx context.Context, provider chat.Provider, opts GenerateOptions) (StreamResult, error) {
	if provider == nil {
		return StreamResult{}, ErrNoProvider
	}

	src := make(chan StreamPart, streamBufferSize)
	full := make(chan StreamPart, streamBufferSize)
	text := make(chan string, streamBufferSize)
	done := make(chan struct{})

	// Producer-goroutine-owned values, published to the futures via the
	// happens-before guarantee of close(done).
	var (
		finalUsage  chat.Usage
		finalReason FinishReason
		runErr      error
	)

	// Tee: read from src, fan out to full (synchronous) and text
	// (best-effort drop on backpressure).
	go func() {
		defer close(full)
		defer close(text)
		for p := range src {
			full <- p
			if p.Type == StreamPartTextDelta && p.TextDelta != "" {
				select {
				case text <- p.TextDelta:
				default:
				}
			}
		}
	}()

	go func() {
		defer close(src)
		defer close(done)

		stop := effectiveStopCondition(opts)
		messages := buildBaseMessages(opts)
		wireTools := toolsToChat(opts.Tools)

		var (
			steps      []StepResult
			totalUsage chat.Usage
			lastReason FinishReason
		)

		emit := func(p StreamPart) bool {
			select {
			case src <- p:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for stepNum := 0; ; stepNum++ {
			if err := ctx.Err(); err != nil {
				runErr = fmt.Errorf("%w: %v", ErrAborted, err)
				finalReason = FinishReasonError
				_ = emit(StreamPart{Type: StreamPartAbort, Error: runErr})
				return
			}

			if !emit(StreamPart{Type: StreamPartStartStep}) {
				runErr = fmt.Errorf("%w: %v", ErrAborted, ctx.Err())
				finalReason = FinishReasonError
				return
			}

			req := chat.Request{
				Model:           opts.Model,
				Messages:        messages,
				MaxTokens:       opts.MaxTokens,
				Temperature:     opts.Temperature,
				Tools:           wireTools,
				ProviderOptions: opts.ProviderOptions,
			}

			stream, err := provider.ChatStream(ctx, req)
			if err != nil {
				runErr = err
				finalReason = FinishReasonError
				_ = emit(StreamPart{Type: StreamPartError, Error: err})
				return
			}

			var (
				stepText      string
				stepReasoning string
				toolDeltas    []chat.ToolCallDelta
				stepUsage     chat.Usage
				stepReason    string
				stepWarnings  []chat.Warning
			)

			for {
				chunk, cerr := stream.Next(ctx)
				if errors.Is(cerr, io.EOF) {
					break
				}
				if cerr != nil {
					_ = stream.Close()
					runErr = cerr
					finalReason = FinishReasonError
					_ = emit(StreamPart{Type: StreamPartError, Error: cerr})
					return
				}
				if chunk.Delta != "" {
					stepText += chunk.Delta
					if !emit(StreamPart{Type: StreamPartTextDelta, TextDelta: chunk.Delta}) {
						_ = stream.Close()
						runErr = fmt.Errorf("%w: %v", ErrAborted, ctx.Err())
						finalReason = FinishReasonError
						return
					}
				}
				if chunk.ReasoningDelta != "" {
					stepReasoning += chunk.ReasoningDelta
					if !emit(StreamPart{Type: StreamPartReasoningDelta, ReasoningDelta: chunk.ReasoningDelta}) {
						_ = stream.Close()
						runErr = fmt.Errorf("%w: %v", ErrAborted, ctx.Err())
						finalReason = FinishReasonError
						return
					}
				}
				if len(chunk.Warnings) > 0 {
					stepWarnings = append(stepWarnings, chunk.Warnings...)
					for i := range chunk.Warnings {
						w := chunk.Warnings[i]
						if !emit(StreamPart{Type: StreamPartWarning, Warning: &w}) {
							_ = stream.Close()
							runErr = fmt.Errorf("%w: %v", ErrAborted, ctx.Err())
							finalReason = FinishReasonError
							return
						}
					}
				}
				if len(chunk.ToolCallDeltas) > 0 {
					toolDeltas = append(toolDeltas, chunk.ToolCallDeltas...)
				}
				if chunk.Usage != nil {
					stepUsage = *chunk.Usage
				}
				if chunk.FinishReason != "" {
					stepReason = chunk.FinishReason
				}
				if chunk.Done {
					break
				}
			}
			_ = stream.Close()

			assembled := chat.AssembleToolCalls(toolDeltas)
			coreCalls := toCoreToolCalls(assembled)
			reason := mapFinishReason(stepReason)
			if len(coreCalls) > 0 && reason != FinishReasonToolCalls {
				reason = FinishReasonToolCalls
			}

			for i := range coreCalls {
				tc := coreCalls[i]
				if !emit(StreamPart{Type: StreamPartToolCall, ToolCall: &tc}) {
					runErr = fmt.Errorf("%w: %v", ErrAborted, ctx.Err())
					finalReason = FinishReasonError
					return
				}
			}

			messages = append(messages, assistantMessageFromCalls(stepText, stepReasoning, assembled))

			// Build canonical Parts for this step: reasoning first, then text.
			var stepParts chat.Parts
			if stepReasoning != "" {
				stepParts = append(stepParts, chat.ReasoningPart{Text: stepReasoning})
			}
			if stepText != "" {
				stepParts = append(stepParts, chat.TextPart{Text: stepText})
			}

			step := StepResult{
				StepNumber:   stepNum,
				FinishReason: reason,
				Text:         stepText,
				Parts:        stepParts,
				Reasoning:    stepReasoning,
				ToolCalls:    coreCalls,
				Usage:        stepUsage,
				Warnings:     stepWarnings,
			}

			if len(coreCalls) > 0 {
				results, toolMsgs := executeToolCalls(ctx, coreCalls, opts.Tools)
				step.ToolResults = results
				for i := range results {
					r := results[i]
					if !emit(StreamPart{Type: StreamPartToolResult, ToolResult: &r}) {
						runErr = fmt.Errorf("%w: %v", ErrAborted, ctx.Err())
						finalReason = FinishReasonError
						return
					}
				}
				messages = append(messages, toolMsgs...)
			}

			steps = append(steps, step)
			totalUsage = addUsage(totalUsage, stepUsage)
			lastReason = reason

			sCopy := step
			if !emit(StreamPart{Type: StreamPartFinishStep, StepResult: &sCopy}) {
				runErr = fmt.Errorf("%w: %v", ErrAborted, ctx.Err())
				finalReason = FinishReasonError
				return
			}

			if len(coreCalls) == 0 || stop(steps) {
				break
			}
		}

		finalUsage = totalUsage
		finalReason = lastReason
		usageCopy := totalUsage
		_ = emit(StreamPart{
			Type:         StreamPartFinish,
			FinishReason: lastReason,
			TotalUsage:   &usageCopy,
		})
	}()

	usage := UsageFuture(func() (chat.Usage, error) {
		<-done
		return finalUsage, runErr
	})
	reasonFuture := ReasonFuture(func() (FinishReason, error) {
		<-done
		return finalReason, runErr
	})

	return StreamResult{
		FullStream:   full,
		TextStream:   text,
		Usage:        usage,
		FinishReason: reasonFuture,
	}, nil
}
