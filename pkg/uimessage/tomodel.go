package uimessage

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

// ToModelOptions configures ToModelMessages.
type ToModelOptions struct {
	// IgnoreIncompleteToolCalls drops tool parts whose state is
	// input-streaming or input-available.
	IgnoreIncompleteToolCalls bool
}

// ToModelMessages converts UI Messages into chat.Messages suitable for
// driving core.GenerateText / core.StreamText.
func ToModelMessages(msgs []Message, opts ToModelOptions) ([]chat.Message, error) {
	out := make([]chat.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case RoleSystem:
			out = append(out, chat.Message{Role: chat.RoleSystem, Content: collectText(m.Parts)})
		case RoleUser:
			parts, err := userParts(m.Parts)
			if err != nil {
				return nil, fmt.Errorf("user message %s: %w", m.ID, err)
			}
			out = append(out, chat.Message{Role: chat.RoleUser, Parts: parts})
		case RoleAssistant:
			more, err := assistantMessages(m, opts)
			if err != nil {
				return nil, fmt.Errorf("assistant message %s: %w", m.ID, err)
			}
			out = append(out, more...)
		default:
			return nil, fmt.Errorf("unknown role %q", m.Role)
		}
	}
	return out, nil
}

func collectText(ps MessageParts) string {
	var b strings.Builder
	for _, p := range ps {
		if t, ok := p.(TextUIPart); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

func userParts(ps MessageParts) (chat.Parts, error) {
	out := chat.Parts{}
	for _, p := range ps {
		switch v := p.(type) {
		case TextUIPart:
			if v.Text != "" {
				out = append(out, chat.TextPart{Text: v.Text})
			}
		case FileUIPart:
			cp, err := fileToChatPart(v)
			if err != nil {
				return nil, err
			}
			out = append(out, cp)
		}
	}
	return out, nil
}

func assistantMessages(m Message, opts ToModelOptions) ([]chat.Message, error) {
	var out []chat.Message
	parts := chat.Parts{}
	var toolCalls []chat.ToolCall
	var pendingResults []chat.Message

	flush := func() {
		if len(parts) > 0 || len(toolCalls) > 0 {
			out = append(out, chat.Message{
				Role:      chat.RoleAssistant,
				Parts:     parts,
				ToolCalls: toolCalls,
			})
			parts = chat.Parts{}
			toolCalls = nil
		}
		out = append(out, pendingResults...)
		pendingResults = nil
	}

	for _, p := range m.Parts {
		switch v := p.(type) {
		case TextUIPart:
			if v.Text != "" {
				parts = append(parts, chat.TextPart{Text: v.Text})
			}
		case ReasoningUIPart:
			if v.Text != "" {
				parts = append(parts, chat.ReasoningPart{Text: v.Text, ProviderMetadata: v.ProviderMetadata})
			}
		case FileUIPart:
			cp, err := fileToChatPart(v)
			if err != nil {
				return nil, err
			}
			parts = append(parts, cp)
		case ToolUIPart:
			if !includeTool(v.State, opts) {
				continue
			}
			args, err := toJSONString(v.Input)
			if err != nil {
				return nil, err
			}
			toolCalls = append(toolCalls, chat.ToolCall{ID: v.ToolCallID, Name: v.ToolName, Arguments: args})
			if v.State == ToolStateOutputAvailable || v.State == ToolStateOutputError {
				rm, err := toolResultMessage(v.ToolCallID, v.ToolName, v.Output, v.ErrorText)
				if err != nil {
					return nil, err
				}
				pendingResults = append(pendingResults, rm)
			}
		case DynamicToolUIPart:
			if !includeTool(v.State, opts) {
				continue
			}
			args, err := toJSONString(v.Input)
			if err != nil {
				return nil, err
			}
			toolCalls = append(toolCalls, chat.ToolCall{ID: v.ToolCallID, Name: v.ToolName, Arguments: args})
			if v.State == ToolStateOutputAvailable || v.State == ToolStateOutputError {
				rm, err := toolResultMessage(v.ToolCallID, v.ToolName, v.Output, v.ErrorText)
				if err != nil {
					return nil, err
				}
				pendingResults = append(pendingResults, rm)
			}
		case StepStartUIPart:
			flush()
		}
	}
	flush()
	return out, nil
}

func fileToChatPart(v FileUIPart) (chat.Part, error) {
	data, mediaType, isData, err := decodeMaybeDataURL(v.URL, v.MediaType)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(mediaType, "image/") {
		ip := chat.ImagePart{MediaType: mediaType}
		if isData {
			ip.Data = data
		} else {
			ip.URL = v.URL
		}
		return ip, nil
	}
	fp := chat.FilePart{MediaType: mediaType, Name: v.Filename}
	if isData {
		fp.Data = data
	} else {
		fp.URL = v.URL
	}
	return fp, nil
}

func includeTool(state ToolPartState, opts ToModelOptions) bool {
	if !opts.IgnoreIncompleteToolCalls {
		return true
	}
	return state != ToolStateInputStreaming && state != ToolStateInputAvailable
}

func toolResultMessage(callID, toolName string, output any, errText string) (chat.Message, error) {
	var content string
	if errText != "" {
		content = errText
	} else {
		s, err := toJSONString(output)
		if err != nil {
			return chat.Message{}, err
		}
		content = s
	}
	return chat.Message{
		Role:       chat.RoleTool,
		Name:       toolName,
		ToolCallID: callID,
		Content:    content,
	}, nil
}

func toJSONString(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	if s, ok := v.(string); ok {
		var probe any
		if err := json.Unmarshal([]byte(s), &probe); err == nil {
			return s, nil
		}
		b, err := json.Marshal(s)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// decodeMaybeDataURL recognises data:[<mediatype>];base64,<data> URLs.
func decodeMaybeDataURL(url, fallbackMediaType string) ([]byte, string, bool, error) {
	if !strings.HasPrefix(url, "data:") {
		return nil, fallbackMediaType, false, nil
	}
	rest := strings.TrimPrefix(url, "data:")
	semi := strings.Index(rest, ",")
	if semi < 0 {
		return nil, "", false, fmt.Errorf("invalid data URL: missing comma")
	}
	meta, body := rest[:semi], rest[semi+1:]
	mediaType := fallbackMediaType
	isB64 := false
	for _, tok := range strings.Split(meta, ";") {
		tok = strings.TrimSpace(tok)
		if tok == "base64" {
			isB64 = true
		} else if tok != "" {
			mediaType = tok
		}
	}
	if !isB64 {
		return []byte(body), mediaType, true, nil
	}
	data, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, "", false, fmt.Errorf("decode data URL: %w", err)
	}
	return data, mediaType, true, nil
}
