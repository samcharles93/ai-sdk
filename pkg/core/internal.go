package core

import (
	"strings"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

// mapFinishReason translates a provider-level finish reason string into
// the canonical [FinishReason] vocabulary. Both underscore and hyphen
// spellings are accepted. An empty string maps to [FinishReasonStop] —
// providers that complete without an explicit reason are treated as a
// natural stop.
func mapFinishReason(s string) FinishReason {
	switch s {
	case "", "stop", "end_turn":
		return FinishReasonStop
	case "length", "max_tokens":
		return FinishReasonLength
	case "content_filter", "content-filter", "safety":
		return FinishReasonContentFilter
	case "tool_calls", "tool-calls", "tool_use":
		return FinishReasonToolCalls
	case "error":
		return FinishReasonError
	default:
		return FinishReasonOther
	}
}

// addUsage returns the element-wise sum of two [chat.Usage] values.
func addUsage(a, b chat.Usage) chat.Usage {
	return chat.Usage{
		PromptTokens:     a.PromptTokens + b.PromptTokens,
		CompletionTokens: a.CompletionTokens + b.CompletionTokens,
		TotalTokens:      a.TotalTokens + b.TotalTokens,
	}
}

// buildBaseMessages constructs the initial message slice from opts:
// optional system message, opts.Messages, then optional simple prompt.
func buildBaseMessages(opts GenerateOptions) []chat.Message {
	out := make([]chat.Message, 0, len(opts.Messages)+2)
	if opts.System != "" {
		out = append(out, chat.Message{Role: chat.RoleSystem, Content: opts.System})
	}
	out = append(out, opts.Messages...)
	if opts.Prompt != "" {
		out = append(out, chat.Message{Role: chat.RoleUser, Content: opts.Prompt})
	}
	return out
}

// toolsToChat converts a [ToolSet] into the wire-level [chat.Tool] slice
// that providers consume. Execute functions are intentionally dropped:
// the wire definition only needs name/description/parameters.
//
// Returned tools are ordered by map iteration; callers that depend on a
// stable order should sort externally. Tests in this package sort before
// asserting.
func toolsToChat(set ToolSet) []chat.Tool {
	if len(set) == 0 {
		return nil
	}
	out := make([]chat.Tool, 0, len(set))
	for _, t := range set {
		if t == nil {
			continue
		}
		out = append(out, chat.Tool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}
	return out
}

// toCoreToolCalls converts wire-level [chat.ToolCall] entries into the
// public [ToolCall] type exposed by this package.
func toCoreToolCalls(in []chat.ToolCall) []ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]ToolCall, len(in))
	for i, c := range in {
		out[i] = ToolCall{ToolCallID: c.ID, ToolName: c.Name, Input: c.Arguments}
	}
	return out
}

// effectiveStopCondition returns the stop condition to use for a run.
// If StopWhen is set, it takes precedence; otherwise StepCountIs(MaxSteps)
// is used, with MaxSteps coerced to a minimum of 1.
func effectiveStopCondition(opts GenerateOptions) StopCondition {
	if opts.StopWhen != nil {
		return opts.StopWhen
	}
	maxSteps := max(opts.MaxSteps, 1)
	return StepCountIs(maxSteps)
}

// partsReasoning concatenates the text of every [chat.ReasoningPart] in
// parts, preserving order. Returns the empty string when no reasoning is
// present (or parts is nil).
func partsReasoning(parts chat.Parts) string {
	if len(parts) == 0 {
		return ""
	}
	var out strings.Builder
	for _, p := range parts {
		if rp, ok := p.(chat.ReasoningPart); ok {
			out.WriteString(rp.Text)
		}
	}
	return out.String()
}
