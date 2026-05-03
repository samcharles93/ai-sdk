package uimessage

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Chunk is a single event in a UI message stream. Concrete chunk types
// implement this interface; the wire format is a JSON object with a
// "type" discriminator string matching TypeName.
type Chunk interface {
	// TypeName returns the chunk's wire-format "type" value
	// (e.g. "text-delta", "tool-input-start", "data-weather").
	TypeName() string
	// isChunk seals the interface.
	isChunk()
}

// FinishReason mirrors core.FinishReason for the wire protocol.
type FinishReason string

// Standard finish reasons. Values match the TS UI message stream.
const (
	FinishReasonStop          FinishReason = "stop"
	FinishReasonLength        FinishReason = "length"
	FinishReasonContentFilter FinishReason = "content-filter"
	FinishReasonToolCalls     FinishReason = "tool-calls"
	FinishReasonError         FinishReason = "error"
	FinishReasonOther         FinishReason = "other"
)

// ProviderMetadata is opaque per-provider metadata carried on chunks
// and parts.
type ProviderMetadata = map[string]any

// --- text ----------------------------------------------------------------

type TextStartChunk struct {
	ID               string           `json:"id"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (TextStartChunk) TypeName() string { return "text-start" }
func (TextStartChunk) isChunk()         {}

type TextDeltaChunk struct {
	ID               string           `json:"id"`
	Delta            string           `json:"delta"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (TextDeltaChunk) TypeName() string { return "text-delta" }
func (TextDeltaChunk) isChunk()         {}

type TextEndChunk struct {
	ID               string           `json:"id"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (TextEndChunk) TypeName() string { return "text-end" }
func (TextEndChunk) isChunk()         {}

// --- reasoning -----------------------------------------------------------

type ReasoningStartChunk struct {
	ID               string           `json:"id"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (ReasoningStartChunk) TypeName() string { return "reasoning-start" }
func (ReasoningStartChunk) isChunk()         {}

type ReasoningDeltaChunk struct {
	ID               string           `json:"id"`
	Delta            string           `json:"delta"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (ReasoningDeltaChunk) TypeName() string { return "reasoning-delta" }
func (ReasoningDeltaChunk) isChunk()         {}

type ReasoningEndChunk struct {
	ID               string           `json:"id"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (ReasoningEndChunk) TypeName() string { return "reasoning-end" }
func (ReasoningEndChunk) isChunk()         {}

// --- error ---------------------------------------------------------------

type ErrorChunk struct {
	ErrorText string `json:"errorText"`
}

func (ErrorChunk) TypeName() string { return "error" }
func (ErrorChunk) isChunk()         {}

// --- tool input ----------------------------------------------------------

type ToolInputStartChunk struct {
	ToolCallID       string           `json:"toolCallId"`
	ToolName         string           `json:"toolName"`
	ProviderExecuted *bool            `json:"providerExecuted,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
	Dynamic          *bool            `json:"dynamic,omitempty"`
	Title            string           `json:"title,omitempty"`
}

func (ToolInputStartChunk) TypeName() string { return "tool-input-start" }
func (ToolInputStartChunk) isChunk()         {}

type ToolInputDeltaChunk struct {
	ToolCallID     string `json:"toolCallId"`
	InputTextDelta string `json:"inputTextDelta"`
}

func (ToolInputDeltaChunk) TypeName() string { return "tool-input-delta" }
func (ToolInputDeltaChunk) isChunk()         {}

type ToolInputAvailableChunk struct {
	ToolCallID       string           `json:"toolCallId"`
	ToolName         string           `json:"toolName"`
	Input            any              `json:"input"`
	ProviderExecuted *bool            `json:"providerExecuted,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
	Dynamic          *bool            `json:"dynamic,omitempty"`
	Title            string           `json:"title,omitempty"`
}

func (ToolInputAvailableChunk) TypeName() string { return "tool-input-available" }
func (ToolInputAvailableChunk) isChunk()         {}

type ToolInputErrorChunk struct {
	ToolCallID       string           `json:"toolCallId"`
	ToolName         string           `json:"toolName"`
	Input            any              `json:"input"`
	ProviderExecuted *bool            `json:"providerExecuted,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
	Dynamic          *bool            `json:"dynamic,omitempty"`
	ErrorText        string           `json:"errorText"`
	Title            string           `json:"title,omitempty"`
}

func (ToolInputErrorChunk) TypeName() string { return "tool-input-error" }
func (ToolInputErrorChunk) isChunk()         {}

// --- tool approval -------------------------------------------------------

type ToolApprovalRequestChunk struct {
	ApprovalID  string `json:"approvalId"`
	ToolCallID  string `json:"toolCallId"`
	IsAutomatic *bool  `json:"isAutomatic,omitempty"`
}

func (ToolApprovalRequestChunk) TypeName() string { return "tool-approval-request" }
func (ToolApprovalRequestChunk) isChunk()         {}

type ToolApprovalResponseChunk struct {
	ApprovalID       string           `json:"approvalId"`
	Approved         bool             `json:"approved"`
	Reason           string           `json:"reason,omitempty"`
	ProviderExecuted *bool            `json:"providerExecuted,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (ToolApprovalResponseChunk) TypeName() string { return "tool-approval-response" }
func (ToolApprovalResponseChunk) isChunk()         {}

// --- tool output ---------------------------------------------------------

type ToolOutputAvailableChunk struct {
	ToolCallID       string           `json:"toolCallId"`
	Output           any              `json:"output"`
	ProviderExecuted *bool            `json:"providerExecuted,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
	Dynamic          *bool            `json:"dynamic,omitempty"`
	Preliminary      *bool            `json:"preliminary,omitempty"`
}

func (ToolOutputAvailableChunk) TypeName() string { return "tool-output-available" }
func (ToolOutputAvailableChunk) isChunk()         {}

type ToolOutputErrorChunk struct {
	ToolCallID       string           `json:"toolCallId"`
	ErrorText        string           `json:"errorText"`
	ProviderExecuted *bool            `json:"providerExecuted,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
	Dynamic          *bool            `json:"dynamic,omitempty"`
}

func (ToolOutputErrorChunk) TypeName() string { return "tool-output-error" }
func (ToolOutputErrorChunk) isChunk()         {}

type ToolOutputDeniedChunk struct {
	ToolCallID string `json:"toolCallId"`
}

func (ToolOutputDeniedChunk) TypeName() string { return "tool-output-denied" }
func (ToolOutputDeniedChunk) isChunk()         {}

// --- sources / files / custom -------------------------------------------

type SourceURLChunk struct {
	SourceID         string           `json:"sourceId"`
	URL              string           `json:"url"`
	Title            string           `json:"title,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (SourceURLChunk) TypeName() string { return "source-url" }
func (SourceURLChunk) isChunk()         {}

type SourceDocumentChunk struct {
	SourceID         string           `json:"sourceId"`
	MediaType        string           `json:"mediaType"`
	Title            string           `json:"title"`
	Filename         string           `json:"filename,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (SourceDocumentChunk) TypeName() string { return "source-document" }
func (SourceDocumentChunk) isChunk()         {}

type FileChunk struct {
	URL              string           `json:"url"`
	MediaType        string           `json:"mediaType"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (FileChunk) TypeName() string { return "file" }
func (FileChunk) isChunk()         {}

type ReasoningFileChunk struct {
	URL              string           `json:"url"`
	MediaType        string           `json:"mediaType"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (ReasoningFileChunk) TypeName() string { return "reasoning-file" }
func (ReasoningFileChunk) isChunk()         {}

type CustomChunk struct {
	Kind             string           `json:"kind"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (CustomChunk) TypeName() string { return "custom" }
func (CustomChunk) isChunk()         {}

// --- step / lifecycle ---------------------------------------------------

type StartStepChunk struct{}

func (StartStepChunk) TypeName() string { return "start-step" }
func (StartStepChunk) isChunk()         {}

type FinishStepChunk struct{}

func (FinishStepChunk) TypeName() string { return "finish-step" }
func (FinishStepChunk) isChunk()         {}

type StartChunk struct {
	MessageID       string `json:"messageId,omitempty"`
	MessageMetadata any    `json:"messageMetadata,omitempty"`
}

func (StartChunk) TypeName() string { return "start" }
func (StartChunk) isChunk()         {}

type FinishChunk struct {
	FinishReason    FinishReason `json:"finishReason,omitempty"`
	MessageMetadata any          `json:"messageMetadata,omitempty"`
}

func (FinishChunk) TypeName() string { return "finish" }
func (FinishChunk) isChunk()         {}

type AbortChunk struct {
	Reason string `json:"reason,omitempty"`
}

func (AbortChunk) TypeName() string { return "abort" }
func (AbortChunk) isChunk()         {}

type MessageMetadataChunk struct {
	MessageMetadata any `json:"messageMetadata"`
}

func (MessageMetadataChunk) TypeName() string { return "message-metadata" }
func (MessageMetadataChunk) isChunk()         {}

// --- data-${name} -------------------------------------------------------

// DataChunk is a per-application data chunk. The wire-format type is
// "data-<Name>".
type DataChunk struct {
	Name      string `json:"-"`
	ID        string `json:"id,omitempty"`
	Data      any    `json:"data"`
	Transient *bool  `json:"transient,omitempty"`
}

func (d DataChunk) TypeName() string { return "data-" + d.Name }
func (DataChunk) isChunk()           {}

// --- marshal / unmarshal -------------------------------------------------

// MarshalChunk serialises a Chunk to a JSON object with a leading
// "type" field.
func MarshalChunk(c Chunk) ([]byte, error) {
	body, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	t := c.TypeName()
	if string(body) == "{}" {
		return []byte(fmt.Sprintf(`{"type":%q}`, t)), nil
	}
	if len(body) < 2 || body[0] != '{' {
		return nil, fmt.Errorf("uimessage: chunk must marshal to a JSON object, got %s", body)
	}
	out := make([]byte, 0, len(body)+len(t)+10)
	out = append(out, '{')
	out = append(out, fmt.Sprintf(`"type":%q,`, t)...)
	out = append(out, body[1:]...)
	return out, nil
}

// UnmarshalChunk decodes a JSON object into a concrete Chunk by
// dispatching on the "type" discriminator.
func UnmarshalChunk(data []byte) (Chunk, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("uimessage: read chunk type: %w", err)
	}
	switch head.Type {
	case "text-start":
		return decode[TextStartChunk](data)
	case "text-delta":
		return decode[TextDeltaChunk](data)
	case "text-end":
		return decode[TextEndChunk](data)
	case "reasoning-start":
		return decode[ReasoningStartChunk](data)
	case "reasoning-delta":
		return decode[ReasoningDeltaChunk](data)
	case "reasoning-end":
		return decode[ReasoningEndChunk](data)
	case "error":
		return decode[ErrorChunk](data)
	case "tool-input-start":
		return decode[ToolInputStartChunk](data)
	case "tool-input-delta":
		return decode[ToolInputDeltaChunk](data)
	case "tool-input-available":
		return decode[ToolInputAvailableChunk](data)
	case "tool-input-error":
		return decode[ToolInputErrorChunk](data)
	case "tool-approval-request":
		return decode[ToolApprovalRequestChunk](data)
	case "tool-approval-response":
		return decode[ToolApprovalResponseChunk](data)
	case "tool-output-available":
		return decode[ToolOutputAvailableChunk](data)
	case "tool-output-error":
		return decode[ToolOutputErrorChunk](data)
	case "tool-output-denied":
		return decode[ToolOutputDeniedChunk](data)
	case "source-url":
		return decode[SourceURLChunk](data)
	case "source-document":
		return decode[SourceDocumentChunk](data)
	case "file":
		return decode[FileChunk](data)
	case "reasoning-file":
		return decode[ReasoningFileChunk](data)
	case "custom":
		return decode[CustomChunk](data)
	case "start-step":
		return StartStepChunk{}, nil
	case "finish-step":
		return FinishStepChunk{}, nil
	case "start":
		return decode[StartChunk](data)
	case "finish":
		return decode[FinishChunk](data)
	case "abort":
		return decode[AbortChunk](data)
	case "message-metadata":
		return decode[MessageMetadataChunk](data)
	}
	if name, ok := strings.CutPrefix(head.Type, "data-"); ok {
		var d DataChunk
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, fmt.Errorf("uimessage: decode %s: %w", head.Type, err)
		}
		d.Name = name
		return d, nil
	}
	return nil, fmt.Errorf("uimessage: unknown chunk type %q", head.Type)
}

func decode[T Chunk](data []byte) (Chunk, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("uimessage: decode %s: %w", v.TypeName(), err)
	}
	return v, nil
}

// IsDataChunk reports whether c is a DataChunk.
func IsDataChunk(c Chunk) bool {
	_, ok := c.(DataChunk)
	return ok
}
