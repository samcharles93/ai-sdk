package core

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/chat"
)

// GenerateOptions configures a [GenerateText] call.
type GenerateOptions struct {
	// Model is the model identifier passed to the provider.
	Model string
	// System is a system-level instruction.
	System string
	// Prompt is a simple text prompt. Mutually exclusive with Messages.
	Prompt string
	// Messages is a list of prior conversation turns.
	Messages []chat.Message
	// Tools is the set of callable tools.
	Tools ToolSet
	// ToolChoice constrains how the provider selects from Tools. A nil value
	// leaves the provider's default selection behaviour unchanged.
	ToolChoice *chat.ToolChoice
	// MaxSteps limits the number of tool-calling loops. Defaults to 1.
	MaxSteps int
	// MaxParallelToolCalls bounds how many of a step's tool calls execute
	// concurrently. The default (0 or 1) preserves the strictly sequential
	// behaviour: tools are arbitrary user code and some are not safe to run
	// in parallel. Results and tool messages are always returned in call
	// order regardless of completion order.
	MaxParallelToolCalls int
	// Temperature controls sampling randomness.
	Temperature float32
	// MaxTokens limits the total output tokens.
	MaxTokens int
	// StopWhen is an optional stop condition. Defaults to StepCountIs(1).
	StopWhen StopCondition
	// ProviderOptions carries provider-specific options keyed by
	// provider name (e.g. "openai", "anthropic"). These are passed
	// directly to chat.Request.ProviderOptions.
	ProviderOptions map[string]any
	// OnStep, when non-nil, runs before every model call after the first.
	// It receives the full message history about to be sent (including the
	// system message, when opts.System is set) and may return a replacement
	// history for this and subsequent calls — for example, a compacted one
	// once the conversation approaches the context window. Returning nil
	// keeps the current history unchanged. A non-nil error aborts the
	// generation. The hook is not called before the first call.
	OnStep func(ctx context.Context, messages []chat.Message) ([]chat.Message, error)
}

// GenerateText performs a non-streaming text generation with optional
// tool calling. It orchestrates the tool-call loop: calling the model,
// executing any requested tools, and feeding results back until a stop
// condition is met.
//
// This is the Go equivalent of the AI SDK's generateText function.
func GenerateText(ctx context.Context, provider chat.Provider, opts GenerateOptions) (GenerateResult, error) {
	if provider == nil {
		return GenerateResult{}, ErrNoProvider
	}

	stop := effectiveStopCondition(opts)
	messages := buildBaseMessages(opts)
	wireTools := toolsToChat(opts.Tools)

	var (
		steps      []StepResult
		totalUsage chat.Usage
		lastReason FinishReason
	)

	for stepNum := 0; ; stepNum++ {
		if err := ctx.Err(); err != nil {
			return generateResult(steps, totalUsage, lastReason), fmt.Errorf("%w: %w", ErrAborted, err)
		}

		// Between steps, let the caller transform the history before the
		// next model call. Runs only when the loop actually continues, so a
		// caller can compact a history that would otherwise overflow the
		// context window.
		if stepNum > 0 && opts.OnStep != nil {
			next, err := opts.OnStep(ctx, messages)
			if err != nil {
				return generateResult(steps, totalUsage, lastReason), err
			}
			if next != nil {
				messages = next
			}
		}

		req := chat.Request{
			Model:           opts.Model,
			Messages:        messages,
			MaxTokens:       opts.MaxTokens,
			Temperature:     opts.Temperature,
			Tools:           wireTools,
			ToolChoice:      opts.ToolChoice,
			ProviderOptions: opts.ProviderOptions,
		}

		resp, err := provider.Chat(ctx, req)
		if err != nil {
			return generateResult(steps, totalUsage, lastReason), err
		}

		coreCalls := toCoreToolCalls(resp.ToolCalls)
		reason := mapFinishReason(resp.FinishReason)
		// Some providers omit "tool_calls" as the finish reason even when
		// emitting tool calls. Promote when needed so step semantics are
		// consistent across providers.
		if len(coreCalls) > 0 && reason != FinishReasonToolCalls {
			reason = FinishReasonToolCalls
		}

		step := StepResult{
			StepNumber:   stepNum,
			FinishReason: reason,
			Text:         resp.Content,
			Parts:        resp.Parts,
			Reasoning:    partsReasoning(resp.Parts),
			ToolCalls:    coreCalls,
			Usage:        resp.Usage,
			Warnings:     resp.Warnings,
		}

		// Append the assistant turn to the conversation before tool
		// execution so any subsequent step sees it.
		messages = append(messages, assistantMessageFromResponse(resp))

		if len(coreCalls) > 0 {
			results, toolMsgs := executeToolCalls(ctx, coreCalls, opts.Tools, opts.MaxParallelToolCalls)
			step.ToolResults = results
			messages = append(messages, toolMsgs...)
		}

		steps = append(steps, step)
		totalUsage = addUsage(totalUsage, resp.Usage)
		lastReason = reason

		// Termination: stop if the model didn't request tools, or the
		// stop condition fires.
		if len(coreCalls) == 0 || stop(steps) {
			break
		}
	}

	return generateResult(steps, totalUsage, lastReason), nil
}

// generateResult assembles the work completed so far. Error paths use the
// same builder so callers retain already-billed usage and completed steps.
func generateResult(steps []StepResult, totalUsage chat.Usage, lastReason FinishReason) GenerateResult {
	// Aggregate tool calls/results from all steps for the convenience
	// fields on the result.
	var allCalls []ToolCall
	var allResults []ToolResult
	for _, s := range steps {
		allCalls = append(allCalls, s.ToolCalls...)
		allResults = append(allResults, s.ToolResults...)
	}

	finalText := ""
	var finalParts chat.Parts
	var finalReasoning string
	var allWarnings []chat.Warning
	if n := len(steps); n > 0 {
		finalText = steps[n-1].Text
		finalParts = steps[n-1].Parts
		finalReasoning = steps[n-1].Reasoning
	}
	for _, s := range steps {
		allWarnings = append(allWarnings, s.Warnings...)
	}

	return GenerateResult{
		FinishReason: lastReason,
		Text:         finalText,
		Parts:        finalParts,
		Reasoning:    finalReasoning,
		ToolCalls:    allCalls,
		ToolResults:  allResults,
		Steps:        steps,
		TotalUsage:   totalUsage,
		Warnings:     allWarnings,
	}
}
