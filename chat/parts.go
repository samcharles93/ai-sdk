package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// PartType identifies a Part's concrete kind. It is used both for
// type discrimination on the wire ("type" JSON tag) and for callers
// inspecting a Parts slice without a type switch.
type PartType string

// Standard Part kinds.
const (
	PartTypeText      PartType = "text"
	PartTypeImage     PartType = "image"
	PartTypeFile      PartType = "file"
	PartTypeReasoning PartType = "reasoning"
)

// Part is a single content fragment of a Message or Response.
//
// Parts replace the historical "Content string" field as the canonical
// representation of multimodal content. The Part interface is sealed
// (only types defined in this package implement it) so that providers
// can safely type-switch on the concrete kind.
//
// Note on tool calls/results: tool invocations and their outputs are
// currently carried on Message.ToolCalls / Message.ToolCallID rather
// than as parts. This means Parts does not represent a fully-ordered
// transcript when tool calls are interleaved with text in an assistant
// turn. Providers that require strict ordering of content blocks (e.g.
// Anthropic Messages API with thinking signatures) will gain dedicated
// ToolCallPart / ToolResultPart types in a follow-up iteration.
type Part interface {
	// Type returns the Part's discriminator.
	Type() PartType
	// isPart seals the interface.
	isPart()
}

// Parts is an ordered slice of Part. It implements custom JSON
// marshal/unmarshal so that the "type" discriminator round-trips
// correctly through encoding/json.
type Parts []Part

// MarshalJSON encodes the slice as a JSON array of objects, each
// carrying a "type" discriminator alongside the part's fields.
func (ps Parts) MarshalJSON() ([]byte, error) {
	if ps == nil {
		return []byte("null"), nil
	}
	out := make([]json.RawMessage, len(ps))
	for i, p := range ps {
		raw, err := marshalPart(p)
		if err != nil {
			return nil, fmt.Errorf("chat.Parts: marshal index %d: %w", i, err)
		}
		out[i] = raw
	}
	return json.Marshal(out)
}

// UnmarshalJSON decodes a JSON array of part objects back into
// concrete Part values, dispatching on the "type" discriminator.
func (ps *Parts) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*ps = nil
		return nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return fmt.Errorf("chat.Parts: %w", err)
	}
	out := make(Parts, 0, len(raws))
	for i, raw := range raws {
		p, err := unmarshalPart(raw)
		if err != nil {
			return fmt.Errorf("chat.Parts: index %d: %w", i, err)
		}
		out = append(out, p)
	}
	*ps = out
	return nil
}

// Text concatenates the text content of every TextPart, preserving
// order. Non-text parts are skipped entirely. No separator is inserted
// between adjacent text parts; callers that need a lossy textual
// representation including non-text placeholders should iterate Parts
// themselves.
func (ps Parts) Text() string {
	if len(ps) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, p := range ps {
		if t, ok := p.(TextPart); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}

// HasNonText reports whether any part is something other than a
// TextPart. This is the canonical capability check for providers that
// can only handle text and need to emit warnings or errors.
func (ps Parts) HasNonText() bool {
	for _, p := range ps {
		if _, ok := p.(TextPart); !ok {
			return true
		}
	}
	return false
}

// --- concrete parts -------------------------------------------------------

// TextPart is a plain-text content fragment.
type TextPart struct {
	Text string `json:"text"`
}

// Type returns [PartTypeText].
func (TextPart) Type() PartType { return PartTypeText }
func (TextPart) isPart()        {}

// ImagePart is an image content fragment. Exactly one of URL or Data
// must be set. MediaType (e.g. "image/png") is required when Data is
// set and recommended for URLs that lack a discriminating extension.
type ImagePart struct {
	// URL points at a remote image or a data: URI. Mutually exclusive
	// with Data.
	URL string `json:"url,omitempty"`
	// Data carries the raw image bytes. Mutually exclusive with URL.
	Data []byte `json:"data,omitempty"`
	// MediaType is the IANA media type (e.g. "image/png"). Required
	// when Data is set.
	MediaType string `json:"media_type,omitempty"`
	// ProviderMetadata carries provider-specific per-part options
	// (e.g. Anthropic cache control, OpenAI image detail level).
	ProviderMetadata map[string]any `json:"provider_metadata,omitempty"`
}

// Type returns [PartTypeImage].
func (ImagePart) Type() PartType { return PartTypeImage }
func (ImagePart) isPart()        {}

// FilePart is a generic file content fragment (PDF, audio, etc.).
// Exactly one of URL or Data must be set; MediaType is required when
// Data is set.
type FilePart struct {
	URL              string         `json:"url,omitempty"`
	Data             []byte         `json:"data,omitempty"`
	MediaType        string         `json:"media_type,omitempty"`
	Name             string         `json:"name,omitempty"`
	ProviderMetadata map[string]any `json:"provider_metadata,omitempty"`
}

// Type returns [PartTypeFile].
func (FilePart) Type() PartType { return PartTypeFile }
func (FilePart) isPart()        {}

// ReasoningPart carries chain-of-thought / thinking content emitted by
// the model. ProviderMetadata holds opaque provider replay tokens that
// must be preserved verbatim for multi-turn conversations (notably
// Anthropic thinking-block signatures); the SDK does not interpret it.
type ReasoningPart struct {
	Text             string         `json:"text"`
	ProviderMetadata map[string]any `json:"provider_metadata,omitempty"`
}

// Type returns [PartTypeReasoning].
func (ReasoningPart) Type() PartType { return PartTypeReasoning }
func (ReasoningPart) isPart()        {}

// --- constructors ---------------------------------------------------------

// NewTextPart returns a TextPart wrapping text.
func NewTextPart(text string) TextPart { return TextPart{Text: text} }

// NewImageURL constructs an ImagePart from a URL (remote or data: URI).
func NewImageURL(url string) ImagePart { return ImagePart{URL: url} }

// NewImageData constructs an ImagePart from raw bytes plus MediaType.
// MediaType is required; if empty the part is still constructed but
// downstream providers may reject or warn on it.
func NewImageData(mediaType string, data []byte) ImagePart {
	return ImagePart{MediaType: mediaType, Data: data}
}

// NewFileURL constructs a FilePart from a URL.
func NewFileURL(url, mediaType string) FilePart {
	return FilePart{URL: url, MediaType: mediaType}
}

// NewFileData constructs a FilePart from raw bytes.
func NewFileData(name, mediaType string, data []byte) FilePart {
	return FilePart{Name: name, MediaType: mediaType, Data: data}
}

// --- validation -----------------------------------------------------------

// ErrInvalidPart indicates a Part is malformed (e.g. neither URL nor
// Data set on an ImagePart, or both set, or missing MediaType for
// inline data). Provider implementations may wrap this when rejecting
// content.
var ErrInvalidPart = errors.New("chat: invalid part")

// Validate checks the Parts slice for structural validity and returns
// the first error encountered, or nil. It does not enforce
// provider-specific capability limits — those are reported by the
// provider as warnings or errors at request time.
func (ps Parts) Validate() error {
	for i, p := range ps {
		switch v := p.(type) {
		case TextPart:
			// any text accepted (including empty)
		case ImagePart:
			if v.URL == "" && len(v.Data) == 0 {
				return fmt.Errorf("%w: image part %d has neither URL nor Data", ErrInvalidPart, i)
			}
			if v.URL != "" && len(v.Data) != 0 {
				return fmt.Errorf("%w: image part %d has both URL and Data", ErrInvalidPart, i)
			}
			if len(v.Data) != 0 && v.MediaType == "" {
				return fmt.Errorf("%w: image part %d inline data missing MediaType", ErrInvalidPart, i)
			}
		case FilePart:
			if v.URL == "" && len(v.Data) == 0 {
				return fmt.Errorf("%w: file part %d has neither URL nor Data", ErrInvalidPart, i)
			}
			if v.URL != "" && len(v.Data) != 0 {
				return fmt.Errorf("%w: file part %d has both URL and Data", ErrInvalidPart, i)
			}
			if len(v.Data) != 0 && v.MediaType == "" {
				return fmt.Errorf("%w: file part %d inline data missing MediaType", ErrInvalidPart, i)
			}
		case ReasoningPart:
			// any reasoning text accepted
		default:
			return fmt.Errorf("%w: unknown part kind at index %d (%T)", ErrInvalidPart, i, p)
		}
	}
	return nil
}

// --- internal: discriminated-union codec ----------------------------------

// partEnvelope is the wire shape for a single Part: a "type" tag plus
// the part's own fields, flattened. We re-use json.RawMessage so that
// the part's fields are emitted at the top level next to "type".
type partEnvelope struct {
	Type PartType `json:"type"`
}

func marshalPart(p Part) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: nil part", ErrInvalidPart)
	}
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	// Inject the "type" field. body is a JSON object.
	if len(body) == 0 || body[0] != '{' {
		return nil, fmt.Errorf("chat: part %T did not marshal to an object", p)
	}
	tag, err := json.Marshal(string(p.Type()))
	if err != nil {
		return nil, err
	}
	// Build {"type":"<kind>",<rest of body>}.
	out := make([]byte, 0, len(body)+len(tag)+10)
	out = append(out, '{')
	out = append(out, '"', 't', 'y', 'p', 'e', '"', ':')
	out = append(out, tag...)
	if len(body) > 2 { // body has at least one field beyond {}
		out = append(out, ',')
		out = append(out, body[1:]...)
	} else {
		out = append(out, '}')
	}
	return out, nil
}

func unmarshalPart(raw json.RawMessage) (Part, error) {
	var env partEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	switch env.Type {
	case PartTypeText:
		var p TextPart
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case PartTypeImage:
		var p ImagePart
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case PartTypeFile:
		var p FilePart
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case PartTypeReasoning:
		var p ReasoningPart
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "":
		return nil, fmt.Errorf("%w: missing \"type\" discriminator", ErrInvalidPart)
	default:
		return nil, fmt.Errorf("%w: unknown part type %q", ErrInvalidPart, env.Type)
	}
}
