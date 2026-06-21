package core

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/pkg/chat"
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
	// MaxSteps limits the number of tool-calling loops. Defaults to 1.
	MaxSteps int
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
			return GenerateResult{}, fmt.Errorf("%w: %v", ErrAborted, err)
		}

		req := chat.Request{
			Model:       opts.Model,
			Messages:    messages,
			MaxTokens:   opts.MaxTokens,
			Temperature: opts.Temperature,
			Tools:       wireTools,
			ProviderOptions: opts.ProviderOptions,
		}

		resp, err := provider.Chat(ctx, req)
		if err != nil {
			return GenerateResult{}, err
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
			results, toolMsgs := executeToolCalls(ctx, coreCalls, opts.Tools)
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
	}, nil
}
