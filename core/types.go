package core

import (
	"encoding/json"
	"errors"

	"github.com/samcharles93/ai-sdk/chat"
)

// FinishReason describes why the model stopped generating.
type FinishReason string

// Standard finish reasons.
const (
	FinishReasonStop          FinishReason = "stop"
	FinishReasonLength        FinishReason = "length"
	FinishReasonContentFilter FinishReason = "content-filter"
	FinishReasonToolCalls     FinishReason = "tool-calls"
	FinishReasonError         FinishReason = "error"
	FinishReasonOther         FinishReason = "other"
)

// ToolCall represents a single tool invocation requested by the model.
type ToolCall struct {
	// ToolCallID is a provider-assigned identifier for this invocation.
	ToolCallID string `json:"tool_call_id"`
	// ToolName identifies which tool to invoke.
	ToolName string `json:"tool_name"`
	// Input holds the JSON-encoded arguments for the tool.
	Input string `json:"input"`
}

// ToolResult is the outcome of executing a single ToolCall.
type ToolResult struct {
	// ToolCallID matches the originating tool call.
	ToolCallID string `json:"tool_call_id"`
	// ToolName matches the originating tool call.
	ToolName string `json:"tool_name"`
	// Output is the JSON-encoded result returned by the tool.
	Output string `json:"output"`
	// Error is non-empty when tool execution failed.
	Error string `json:"error,omitempty"`
}

// StepResult captures the result of a single step in a multi-step
// generation (one LLM call and its tool executions).
type StepResult struct {
	// StepNumber is the zero-indexed step number.
	StepNumber int `json:"step_number"`
	// FinishReason describes why this step finished.
	FinishReason FinishReason `json:"finish_reason"`
	// Text is the concatenated text content generated in this step.
	Text string `json:"text"`
	// Parts is the canonical multimodal content produced in this step
	// (text + reasoning + future image/file outputs). Text is derived
	// from the TextPart entries; consumers that care about reasoning
	// or non-text content should iterate Parts.
	Parts chat.Parts `json:"parts,omitempty"`
	// Reasoning is the concatenated reasoning/thinking text emitted by
	// the model in this step (if the provider surfaced any). It is also
	// available on Parts as one or more chat.ReasoningPart entries.
	Reasoning string `json:"reasoning,omitempty"`
	// ToolCalls contains the tool calls made by the model in this step.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolResults contains the results of executing tool calls from this step.
	ToolResults []ToolResult `json:"tool_results,omitempty"`
	// Usage reports token consumption for this step.
	Usage chat.Usage `json:"usage"`
	// Warnings contains any non-fatal warnings from the provider.
	Warnings []chat.Warning `json:"warnings,omitempty"`
}

// GenerateResult is the result of a [GenerateText] call.
type GenerateResult struct {
	// FinishReason describes why generation stopped.
	FinishReason FinishReason `json:"finish_reason"`
	// Text is the final text content (concatenation of all step Text).
	Text string `json:"text"`
	// Parts is the canonical multimodal content of the final step.
	// For multi-step runs, only the last step's Parts are exposed here;
	// per-step Parts are available via Steps.
	Parts chat.Parts `json:"parts,omitempty"`
	// Reasoning is the concatenated reasoning text from the final step.
	Reasoning string `json:"reasoning,omitempty"`
	// ToolCalls contains all tool calls across all steps.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolResults contains all tool execution results across all steps.
	ToolResults []ToolResult `json:"tool_results,omitempty"`
	// Steps contains the per-step detail.
	Steps []StepResult `json:"steps"`
	// TotalUsage is the aggregate token usage across all steps.
	TotalUsage chat.Usage `json:"total_usage"`
	// Warnings contains all non-fatal warnings.
	Warnings []chat.Warning `json:"warnings,omitempty"`
}

// StreamResult is the result of a [StreamText] call.
//
// Use the FullStream, TextStream, or Text methods to consume the
// streaming output.
type StreamResult struct {
	// FullStream delivers all stream parts (text deltas, tool calls,
	// tool results, step boundaries, etc.).
	FullStream <-chan StreamPart `json:"-"`
	// TextStream delivers only text deltas.
	TextStream <-chan string `json:"-"`
	// Usage is a future that resolves to the total token usage.
	Usage UsageFuture `json:"-"`
	// FinishReason is a future that resolves to the final finish reason.
	FinishReason ReasonFuture `json:"-"`
}

// UsageFuture is a lazily-resolved total usage value.
type UsageFuture func() (chat.Usage, error)

// ReasonFuture is a lazily-resolved finish reason.
type ReasonFuture func() (FinishReason, error)

// StreamPartType identifies the type of a stream part.
type StreamPartType string

// Stream part type constants.
const (
	StreamPartTextDelta      StreamPartType = "text-delta"
	StreamPartReasoningDelta StreamPartType = "reasoning-delta"
	StreamPartToolCall       StreamPartType = "tool-call"
	StreamPartToolResult     StreamPartType = "tool-result"
	StreamPartStartStep      StreamPartType = "start-step"
	StreamPartFinishStep     StreamPartType = "finish-step"
	StreamPartFinish         StreamPartType = "finish"
	StreamPartError          StreamPartType = "error"
	StreamPartAbort          StreamPartType = "abort"
	StreamPartWarning        StreamPartType = "warning"
)

// StreamPart is a single event in a streaming text generation.
type StreamPart struct {
	// Type identifies the kind of part.
	Type StreamPartType `json:"type"`
	// TextDelta holds incremental text (Type == "text-delta").
	TextDelta string `json:"text_delta,omitempty"`
	// ReasoningDelta holds incremental reasoning text
	// (Type == "reasoning-delta"). Producers that emit reasoning send
	// these between StartStep and the first ToolCall/FinishStep so
	// downstream UI can render thinking blocks before the answer.
	ReasoningDelta string `json:"reasoning_delta,omitempty"`
	// ToolCall holds a new tool call (Type == "tool-call").
	ToolCall *ToolCall `json:"tool_call,omitempty"`
	// ToolResult holds a tool execution outcome (Type == "tool-result").
	ToolResult *ToolResult `json:"tool_result,omitempty"`
	// Warning holds a provider warning (Type == "warning").
	Warning *chat.Warning `json:"warning,omitempty"`
	// Error holds stream-level error details (Type == "error").
	// It is serialised as ErrorString over the wire and reconstructed
	// as errors.New(ErrorString) on deserialisation.
	Error error `json:"-"`
	// ErrorString is the wire-format representation of Error.
	// Use MarshalJSON/UnmarshalJSON to convert to/from Error.
	ErrorString string `json:"error,omitempty"`
	// StepResult holds step-completion data (Type == "finish-step").
	StepResult *StepResult `json:"step_result,omitempty"`
	// FinishReason holds the final reason (Type == "finish").
	FinishReason FinishReason `json:"finish_reason,omitempty"`
	// TotalUsage holds the aggregate usage (Type == "finish").
	TotalUsage *chat.Usage `json:"total_usage,omitempty"`
}

// MarshalJSON serialises a StreamPart to JSON, converting the Error field
// to ErrorString so it is not silently dropped.
func (p StreamPart) MarshalJSON() ([]byte, error) {
	type part StreamPart // avoid recursion
	p.ErrorString = ""
	if p.Error != nil {
		p.ErrorString = p.Error.Error()
	}
	p.Error = nil
	return json.Marshal(part(p))
}

// UnmarshalJSON deserialises a StreamPart from JSON, reconstructing the
// Error field from ErrorString.
func (p *StreamPart) UnmarshalJSON(data []byte) error {
	type part StreamPart // avoid recursion
	if err := json.Unmarshal(data, (*part)(p)); err != nil {
		return err
	}
	if p.ErrorString != "" {
		p.Error = errors.New(p.ErrorString)
	}
	return nil
}
