package uimessage

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MessagePart is a single piece of content within a Message.
type MessagePart interface {
	// PartType returns the wire-format "type" string. For ToolUIPart and
	// DataUIPart this is "tool-<ToolName>" / "data-<Name>".
	PartType() string
	// isMessagePart seals the interface.
	isMessagePart()
}

// PartState is the lifecycle marker for streaming text/reasoning parts.
type PartState string

const (
	PartStateStreaming PartState = "streaming"
	PartStateDone      PartState = "done"
)

// ToolPartState is the state of a ToolUIPart / DynamicToolUIPart.
type ToolPartState string

const (
	ToolStateInputStreaming    ToolPartState = "input-streaming"
	ToolStateInputAvailable    ToolPartState = "input-available"
	ToolStateApprovalRequested ToolPartState = "approval-requested"
	ToolStateApprovalResponded ToolPartState = "approval-responded"
	ToolStateOutputAvailable   ToolPartState = "output-available"
	ToolStateOutputError       ToolPartState = "output-error"
	ToolStateOutputDenied      ToolPartState = "output-denied"
)

// ToolApproval carries approval state on a tool part.
type ToolApproval struct {
	ID          string `json:"id"`
	Approved    *bool  `json:"approved,omitempty"`
	Reason      string `json:"reason,omitempty"`
	IsAutomatic *bool  `json:"isAutomatic,omitempty"`
}

// --- text ----------------------------------------------------------------

type TextUIPart struct {
	Text             string           `json:"text"`
	State            PartState        `json:"state,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (TextUIPart) PartType() string { return "text" }
func (TextUIPart) isMessagePart()   {}

// --- custom --------------------------------------------------------------

type CustomContentUIPart struct {
	Kind             string           `json:"kind"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (CustomContentUIPart) PartType() string { return "custom" }
func (CustomContentUIPart) isMessagePart()   {}

// --- reasoning -----------------------------------------------------------

type ReasoningUIPart struct {
	Text             string           `json:"text"`
	State            PartState        `json:"state,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (ReasoningUIPart) PartType() string { return "reasoning" }
func (ReasoningUIPart) isMessagePart()   {}

// --- sources -------------------------------------------------------------

type SourceURLUIPart struct {
	SourceID         string           `json:"sourceId"`
	URL              string           `json:"url"`
	Title            string           `json:"title,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (SourceURLUIPart) PartType() string { return "source-url" }
func (SourceURLUIPart) isMessagePart()   {}

type SourceDocumentUIPart struct {
	SourceID         string           `json:"sourceId"`
	MediaType        string           `json:"mediaType"`
	Title            string           `json:"title"`
	Filename         string           `json:"filename,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (SourceDocumentUIPart) PartType() string { return "source-document" }
func (SourceDocumentUIPart) isMessagePart()   {}

// --- files ---------------------------------------------------------------

type FileUIPart struct {
	MediaType        string           `json:"mediaType"`
	Filename         string           `json:"filename,omitempty"`
	URL              string           `json:"url"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (FileUIPart) PartType() string { return "file" }
func (FileUIPart) isMessagePart()   {}

type ReasoningFileUIPart struct {
	MediaType        string           `json:"mediaType"`
	URL              string           `json:"url"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (ReasoningFileUIPart) PartType() string { return "reasoning-file" }
func (ReasoningFileUIPart) isMessagePart()   {}

// --- step boundary -------------------------------------------------------

type StepStartUIPart struct{}

func (StepStartUIPart) PartType() string { return "step-start" }
func (StepStartUIPart) isMessagePart()   {}

// --- data ---------------------------------------------------------------

type DataUIPart struct {
	Name string `json:"-"`
	ID   string `json:"id,omitempty"`
	Data any    `json:"data"`
}

func (d DataUIPart) PartType() string { return "data-" + d.Name }
func (DataUIPart) isMessagePart()     {}

// --- tool parts ---------------------------------------------------------

type ToolUIPart struct {
	ToolName               string           `json:"-"`
	ToolCallID             string           `json:"toolCallId"`
	State                  ToolPartState    `json:"state"`
	Title                  string           `json:"title,omitempty"`
	Input                  any              `json:"input,omitempty"`
	Output                 any              `json:"output,omitempty"`
	RawInput               any              `json:"rawInput,omitempty"`
	ErrorText              string           `json:"errorText,omitempty"`
	Preliminary            *bool            `json:"preliminary,omitempty"`
	ProviderExecuted       *bool            `json:"providerExecuted,omitempty"`
	CallProviderMetadata   ProviderMetadata `json:"callProviderMetadata,omitempty"`
	ResultProviderMetadata ProviderMetadata `json:"resultProviderMetadata,omitempty"`
	Approval               *ToolApproval    `json:"approval,omitempty"`
}

func (t ToolUIPart) PartType() string { return "tool-" + t.ToolName }
func (ToolUIPart) isMessagePart()     {}

type DynamicToolUIPart struct {
	ToolName               string           `json:"toolName"`
	ToolCallID             string           `json:"toolCallId"`
	State                  ToolPartState    `json:"state"`
	Title                  string           `json:"title,omitempty"`
	Input                  any              `json:"input,omitempty"`
	Output                 any              `json:"output,omitempty"`
	ErrorText              string           `json:"errorText,omitempty"`
	Preliminary            *bool            `json:"preliminary,omitempty"`
	ProviderExecuted       *bool            `json:"providerExecuted,omitempty"`
	CallProviderMetadata   ProviderMetadata `json:"callProviderMetadata,omitempty"`
	ResultProviderMetadata ProviderMetadata `json:"resultProviderMetadata,omitempty"`
	Approval               *ToolApproval    `json:"approval,omitempty"`
}

func (DynamicToolUIPart) PartType() string { return "dynamic-tool" }
func (DynamicToolUIPart) isMessagePart()   {}

// --- type guards --------------------------------------------------------

// IsToolUIPart reports whether p is a static or dynamic tool part.
func IsToolUIPart(p MessagePart) bool {
	switch p.(type) {
	case ToolUIPart, DynamicToolUIPart:
		return true
	}
	return false
}

// ToolCallID returns the toolCallId for tool parts, or "".
func ToolCallID(p MessagePart) string {
	switch v := p.(type) {
	case ToolUIPart:
		return v.ToolCallID
	case DynamicToolUIPart:
		return v.ToolCallID
	}
	return ""
}

// MessageParts is an ordered slice of MessagePart with custom JSON
// encoding/decoding that handles the type discriminator and the
// tool-<name> / data-<name> prefixed wire types.
type MessageParts []MessagePart

func (ps MessageParts) MarshalJSON() ([]byte, error) {
	if ps == nil {
		return []byte("null"), nil
	}
	out := make([]json.RawMessage, len(ps))
	for i, p := range ps {
		raw, err := marshalPart(p)
		if err != nil {
			return nil, fmt.Errorf("uimessage: marshal part %d: %w", i, err)
		}
		out[i] = raw
	}
	return json.Marshal(out)
}

func (ps *MessageParts) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*ps = nil
		return nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return fmt.Errorf("uimessage: parts: %w", err)
	}
	out := make(MessageParts, 0, len(raws))
	for i, raw := range raws {
		p, err := unmarshalPart(raw)
		if err != nil {
			return fmt.Errorf("uimessage: parts[%d]: %w", i, err)
		}
		out = append(out, p)
	}
	*ps = out
	return nil
}

func marshalPart(p MessagePart) ([]byte, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	t := p.PartType()
	if string(body) == "{}" {
		return fmt.Appendf(nil, `{"type":%q}`, t), nil
	}
	if len(body) < 2 || body[0] != '{' {
		return nil, fmt.Errorf("part %q must marshal to JSON object, got %s", t, body)
	}
	out := make([]byte, 0, len(body)+len(t)+10)
	out = append(out, '{')
	out = append(out, fmt.Sprintf(`"type":%q,`, t)...)
	out = append(out, body[1:]...)
	return out, nil
}

func unmarshalPart(data []byte) (MessagePart, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case "text":
		return decodePart[TextUIPart](data)
	case "custom":
		return decodePart[CustomContentUIPart](data)
	case "reasoning":
		return decodePart[ReasoningUIPart](data)
	case "source-url":
		return decodePart[SourceURLUIPart](data)
	case "source-document":
		return decodePart[SourceDocumentUIPart](data)
	case "file":
		return decodePart[FileUIPart](data)
	case "reasoning-file":
		return decodePart[ReasoningFileUIPart](data)
	case "step-start":
		return StepStartUIPart{}, nil
	case "dynamic-tool":
		return decodePart[DynamicToolUIPart](data)
	}
	if name, ok := strings.CutPrefix(head.Type, "tool-"); ok {
		var p ToolUIPart
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		p.ToolName = name
		return p, nil
	}
	if name, ok := strings.CutPrefix(head.Type, "data-"); ok {
		var p DataUIPart
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		p.Name = name
		return p, nil
	}
	return nil, fmt.Errorf("unknown part type %q", head.Type)
}

func decodePart[T MessagePart](data []byte) (MessagePart, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return v, nil
}
