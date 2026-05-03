package chat

import "encoding/json"

// Role identifies the author of a Message in a chat conversation.
type Role string

// Standard chat roles. Providers map these to their own role vocabularies.
const (
	// RoleSystem is used for high-level instructions or persona setup.
	RoleSystem Role = "system"
	// RoleUser is used for end-user messages.
	RoleUser Role = "user"
	// RoleAssistant is used for model-generated replies.
	RoleAssistant Role = "assistant"
	// RoleTool is used for tool/function call results fed back to the model.
	RoleTool Role = "tool"
)

// Message is a single entry in a chat conversation.
//
// Multimodal content is carried on [Message.Parts]. The legacy
// [Message.Content] string remains for ergonomic text-only construction
// — when Parts is nil, providers treat Content as a single TextPart;
// when Parts is non-nil, Parts is canonical and Content is ignored on
// the request path. On the response path, providers populate both
// fields: Parts as the source of truth, Content as the concatenation of
// all TextPart text for back-compat.
//
// Tool integration uses two additional fields:
//
//   - Assistant messages that called tools carry [Message.ToolCalls];
//     Content/Parts may also be present alongside tool calls for some
//     providers.
//   - Messages with [RoleTool] carry the tool's output in Content and
//     reference the originating call via [Message.ToolCallID].
//
// ProviderOptions allows per-message provider-specific options keyed by
// provider name, mirroring [Request.ProviderOptions]. This is rarely
// needed at the call site but is useful for things like Anthropic's
// per-block cache control or OpenAI's per-message attachments.
type Message struct {
	Role            Role           `json:"role"`
	Content         string         `json:"content,omitempty"`
	Parts           Parts          `json:"parts,omitempty"`
	Name            string         `json:"name,omitempty"`
	ToolCalls       []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID      string         `json:"tool_call_id,omitempty"`
	ProviderOptions map[string]any `json:"provider_options,omitempty"`
}

// GetParts returns the canonical Parts slice for the message:
//
//   - If Parts is non-nil, it is returned as-is.
//   - Otherwise, if Content is non-empty, a single-element slice
//     containing a TextPart is returned.
//   - Otherwise, nil is returned.
//
// Provider implementations should call GetParts (not access
// Message.Content directly) so that callers using either field are
// handled uniformly.
func (m Message) GetParts() Parts {
	if m.Parts != nil {
		return m.Parts
	}
	if m.Content != "" {
		return Parts{TextPart{Text: m.Content}}
	}
	return nil
}

// Text returns the textual content of the message: Parts.Text() if
// Parts is non-nil, otherwise Content.
func (m Message) Text() string {
	if m.Parts != nil {
		return m.Parts.Text()
	}
	return m.Content
}

// Tool describes a callable tool/function that the model may invoke.
//
// Parameters is a JSON Schema (RFC 8927-compatible / OpenAPI-compatible)
// describing the tool's expected input. Providers translate Tool into
// their wire format (OpenAI/DeepSeek "function", Gemini "functionDeclaration",
// Ollama "tools").
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall is a single tool invocation requested by the model.
//
// ID is the provider-assigned identifier used to match a subsequent
// [RoleTool] message to this call. Some providers (notably Ollama) do
// not emit IDs; in that case the SDK synthesises one (call_<index>) so
// that downstream code can correlate calls and results uniformly.
//
// Arguments is the JSON-encoded argument object as a string, matching
// the wire shape used by every provider we target.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolChoiceType controls how the model decides which (if any) tool to call.
type ToolChoiceType string

const (
	// ToolChoiceAuto lets the model choose freely (default when tools are present).
	ToolChoiceAuto ToolChoiceType = "auto"
	// ToolChoiceNone forbids tool calls; the model must answer with text.
	ToolChoiceNone ToolChoiceType = "none"
	// ToolChoiceRequired forces the model to call any tool.
	ToolChoiceRequired ToolChoiceType = "required"
	// ToolChoiceTool forces the model to call a specific named tool.
	ToolChoiceTool ToolChoiceType = "tool"
)

// ToolChoice constrains the model's tool selection behaviour. When Type is
// [ToolChoiceTool], Name identifies the required tool.
type ToolChoice struct {
	Type ToolChoiceType `json:"type"`
	Name string         `json:"name,omitempty"`
}

// Request is a provider-agnostic chat completion request.
//
// Only Model and Messages are required; all other fields are optional and
// providers should treat zero values as "unspecified" and apply their own
// defaults.
//
// ProviderOptions carries provider-specific options keyed by provider
// name (for example "openai", "anthropic", "ollama"). Each provider
// reads only its own bucket and ignores keys for other providers, so the
// same Request can be shared across providers without modification. Use
// the [ProviderOptionsFor] helper to extract a typed options struct from
// inside a provider implementation.
type Request struct {
	Model           string            `json:"model"`
	Messages        []Message         `json:"messages"`
	MaxTokens       int               `json:"max_tokens,omitempty"`
	Temperature     float32           `json:"temperature,omitempty"`
	TopP            float32           `json:"top_p,omitempty"`
	Stop            []string          `json:"stop,omitempty"`
	Stream          bool              `json:"stream,omitempty"`
	Tools           []Tool            `json:"tools,omitempty"`
	ToolChoice      *ToolChoice       `json:"tool_choice,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	ProviderOptions map[string]any    `json:"provider_options,omitempty"`
}

// Usage reports token accounting for a chat completion.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Response is a non-streaming chat completion result.
//
// When the model invoked tools, ToolCalls is non-empty and FinishReason
// is "tool_calls". Content may still be present alongside tool calls for
// some providers.
//
// Parts is the canonical multimodal representation of the assistant's
// reply (text + reasoning + future image/file outputs). Content is
// populated as the concatenation of all TextPart text for back-compat
// and ergonomic text-only consumption.
type Response struct {
	ID           string     `json:"id,omitempty"`
	Model        string     `json:"model,omitempty"`
	Role         Role       `json:"role,omitempty"`
	Content      string     `json:"content"`
	Parts        Parts      `json:"parts,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason,omitempty"`
	Usage        Usage      `json:"usage"`
	Warnings     []Warning  `json:"warnings,omitempty"`
}

// Warning is a non-fatal provider message (e.g. "image part dropped:
// model is text-only"). Providers attach warnings to Response.Warnings
// or Chunk.Warnings; core aggregates them onto StepResult/GenerateResult.
type Warning struct {
	// Message is the human-readable warning text.
	Message string `json:"message"`
	// Type is an optional machine-readable category, e.g.
	// "unsupported-content", "deprecated-option".
	Type string `json:"type,omitempty"`
}

// ToolCallDelta is an incremental update to a single tool call within a
// streaming response. Multiple parallel tool calls are distinguished by
// Index.
//
// On the first delta for a given Index, ID and Name are populated; on
// subsequent deltas only ArgsDelta is appended (the JSON arguments
// arrive token-by-token). Consumers concatenate ArgsDelta across all
// deltas with the same Index to reconstruct the full Arguments string.
//
// Some providers (Ollama) emit a complete tool call in a single chunk
// rather than streaming arguments — in that case ID/Name are populated
// and ArgsDelta carries the entire JSON arguments at once.
type ToolCallDelta struct {
	Index     int    `json:"index"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	ArgsDelta string `json:"args_delta,omitempty"`
}

// Chunk is a single increment in a streaming chat completion.
//
// Usage is a pointer because token totals are typically only known at the
// end of a stream; intermediate chunks carry Usage == nil and the final
// chunk (Done == true) carries the aggregate usage.
//
// ReasoningDelta carries incremental reasoning/thinking text emitted by
// providers that support it (Anthropic thinking, Gemini thinking, OpenAI
// o1). Providers that do not produce reasoning leave it empty.
type Chunk struct {
	Delta          string          `json:"delta,omitempty"`
	ReasoningDelta string          `json:"reasoning_delta,omitempty"`
	Role           Role            `json:"role,omitempty"`
	ToolCallDeltas []ToolCallDelta `json:"tool_call_deltas,omitempty"`
	FinishReason   string          `json:"finish_reason,omitempty"`
	Usage          *Usage          `json:"usage,omitempty"`
	Warnings       []Warning       `json:"warnings,omitempty"`
	Done           bool            `json:"done,omitempty"`
}
