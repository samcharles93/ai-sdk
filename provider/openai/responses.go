package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/samcharles93/ai-sdk/chat"
)

type responsesAPI struct{}

func (responsesAPI) path() string { return "/responses" }

type responsesUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	PromptTokens       int `json:"prompt_tokens"`
	CompletionTokens   int `json:"completion_tokens"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details,omitempty"`
}

func (u responsesUsage) toUsage() chat.Usage {
	cachedTokens := 0
	if u.InputTokensDetails != nil {
		cachedTokens = u.InputTokensDetails.CachedTokens
	}
	return chat.Usage{
		PromptTokens:     firstNonZero(u.InputTokens, u.PromptTokens),
		CompletionTokens: firstNonZero(u.OutputTokens, u.CompletionTokens),
		TotalTokens:      u.TotalTokens,
		CachedTokens:     cachedTokens,
	}
}

type responsesContent struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type responsesOutputItem struct {
	Type      string             `json:"type"`
	Role      string             `json:"role"`
	Content   []responsesContent `json:"content"`
	Summary   json.RawMessage    `json:"summary"`
	CallID    string             `json:"call_id"`
	Name      string             `json:"name"`
	Arguments string             `json:"arguments"`
}

type responsesResponse struct {
	ID     string                `json:"id"`
	Model  string                `json:"model"`
	Output []responsesOutputItem `json:"output"`
	Usage  responsesUsage        `json:"usage"`
}

func (responsesAPI) buildBody(req chat.Request, stream bool) (map[string]any, []chat.Warning, error) {
	input, warnings, err := buildResponsesInput(req.Messages)
	if err != nil {
		return nil, nil, err
	}
	body := map[string]any{"model": req.Model, "input": input, "stream": stream}
	opts, _ := chat.ProviderOptionsFor[openaiProviderOptions](req.ProviderOptions, "openai")
	if opts.ReasoningEffort != "" {
		reasoning := map[string]any{"effort": opts.ReasoningEffort}
		if opts.ReasoningSummary != "" {
			reasoning["summary"] = opts.ReasoningSummary
		}
		body["reasoning"] = reasoning
	}
	if req.MaxTokens != 0 {
		body["max_output_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, tool := range req.Tools {
			wireTool := map[string]any{"type": "function", "name": tool.Name}
			if tool.Description != "" {
				wireTool["description"] = tool.Description
			}
			if len(tool.Parameters) > 0 {
				wireTool["parameters"] = sanitiseResponsesSchema(tool.Parameters)
			}
			tools[i] = wireTool
		}
		body["tools"] = tools
	}
	if err := applyResponsesToolChoice(body, req.ToolChoice); err != nil {
		return nil, nil, err
	}
	return body, warnings, nil
}

func buildResponsesInput(messages []chat.Message) ([]map[string]any, []chat.Warning, error) {
	var input []map[string]any
	var warnings []chat.Warning
	for i, message := range messages {
		if message.Role == chat.RoleTool {
			if message.ToolCallID == "" {
				return nil, nil, fmt.Errorf("openai: tool message at index %d missing ToolCallID: %w", i, chat.ErrInvalidRequest)
			}
			input = append(input, map[string]any{
				"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Text(),
			})
			continue
		}

		content, messageWarnings := buildResponsesContent(message)
		warnings = append(warnings, messageWarnings...)
		if len(content) > 0 {
			input = append(input, map[string]any{"role": string(message.Role), "content": content})
		}
		if message.Role == chat.RoleAssistant {
			for j, toolCall := range message.ToolCalls {
				id := toolCall.ID
				if id == "" {
					id = fmt.Sprintf("call_%d", j)
				}
				input = append(input, map[string]any{
					"type": "function_call", "call_id": id, "name": toolCall.Name, "arguments": toolCall.Arguments,
				})
			}
		}
	}
	return input, warnings, nil
}

func buildResponsesContent(message chat.Message) ([]map[string]any, []chat.Warning) {
	var content []map[string]any
	var warnings []chat.Warning
	textType := "input_text"
	if message.Role == chat.RoleAssistant {
		textType = "output_text"
	}
	imageType := "input_image"
	for _, part := range message.GetParts() {
		switch part := part.(type) {
		case chat.TextPart:
			if part.Text != "" {
				content = append(content, map[string]any{"type": textType, "text": part.Text})
			}
		case chat.ImagePart:
			if message.Role == chat.RoleAssistant {
				warnings = append(warnings, chat.Warning{Type: "unsupported-content", Message: "openai: ImagePart on assistant Responses input dropped"})
				continue
			}
			imageURL, err := imagePartToURL(part)
			if err != nil {
				warnings = append(warnings, chat.Warning{Type: "invalid-content", Message: fmt.Sprintf("openai: %v", err)})
				continue
			}
			content = append(content, map[string]any{"type": imageType, "image_url": imageURL})
		case chat.FilePart:
			warnings = append(warnings, chat.Warning{Type: "unsupported-content", Message: "openai: FilePart not supported by current models"})
		case chat.ReasoningPart:
			if message.Role == chat.RoleAssistant {
				warnings = append(warnings, chat.Warning{Type: "unsupported-content", Message: "openai: ReasoningPart on assistant input dropped; Responses reasoning replay requires encrypted content"})
			} else if part.Text != "" {
				content = append(content, map[string]any{"type": textType, "text": part.Text})
			}
		}
	}
	return content, warnings
}

func applyResponsesToolChoice(body map[string]any, choice *chat.ToolChoice) error {
	if choice == nil {
		return nil
	}
	switch choice.Type {
	case chat.ToolChoiceAuto:
		body["tool_choice"] = "auto"
	case chat.ToolChoiceNone:
		body["tool_choice"] = "none"
	case chat.ToolChoiceRequired:
		body["tool_choice"] = "required"
	case chat.ToolChoiceTool:
		if choice.Name == "" {
			return fmt.Errorf("openai: tool_choice type=tool requires Name: %w", chat.ErrInvalidRequest)
		}
		body["tool_choice"] = map[string]any{"type": "function", "name": choice.Name}
	default:
		return fmt.Errorf("openai: unknown tool_choice type %q: %w", choice.Type, chat.ErrInvalidRequest)
	}
	return nil
}

func (responsesAPI) decodeResponse(reader io.Reader, warnings []chat.Warning) (chat.Response, error) {
	var wireResponse responsesResponse
	if err := json.NewDecoder(reader).Decode(&wireResponse); err != nil {
		return chat.Response{}, fmt.Errorf("openai: decode responses api response: %w", err)
	}
	response := chat.Response{
		ID: wireResponse.ID, Model: wireResponse.Model, Role: chat.RoleAssistant,
		Warnings: warnings, Usage: wireResponse.Usage.toUsage(),
	}
	for _, item := range wireResponse.Output {
		switch item.Type {
		case "reasoning":
			if summary := decodeResponsesSummary(item.Summary); summary != "" {
				response.Parts = append(response.Parts, chat.ReasoningPart{Text: summary})
			}
		case "function_call":
			response.ToolCalls = append(response.ToolCalls, chat.ToolCall{
				ID: item.CallID, Name: item.Name, Arguments: item.Arguments,
			})
		case "message":
			if item.Role != "" {
				response.Role = chat.Role(item.Role)
			}
			for _, block := range item.Content {
				switch block.Type {
				case "output_text", "text":
					response.Content += block.Text
					response.Parts = append(response.Parts, chat.TextPart{Text: block.Text})
				case "function_call", "tool_call":
					response.ToolCalls = append(response.ToolCalls, chat.ToolCall{
						ID: block.CallID, Name: block.Name, Arguments: block.Arguments,
					})
				}
			}
		}
	}
	if len(response.ToolCalls) > 0 {
		response.FinishReason = "tool_calls"
	}
	return response, nil
}

func decodeResponsesSummary(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, block := range blocks {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "")
}

func (responsesAPI) parseStreamEvent(data []byte) (chat.Chunk, bool, error) {
	var event struct {
		Type      string `json:"type"`
		Delta     string `json:"delta"`
		Text      string `json:"text"`
		Output    string `json:"output"`
		ItemID    string `json:"item_id"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Index     int    `json:"output_index"`
		Item      *struct {
			ID     string `json:"id"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
		} `json:"item"`
		Usage    *responsesUsage `json:"usage"`
		Response *struct {
			Usage *responsesUsage `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return chat.Chunk{}, false, fmt.Errorf("openai: decode responses stream event: %w", err)
	}
	chunk := chat.Chunk{}
	switch event.Type {
	case "response.output_text.delta", "response.text.delta":
		chunk.Delta = firstNonEmpty(event.Delta, event.Text, event.Output)
	case "response.reasoning_text.delta", "response.reasoning.delta":
		chunk.ReasoningDelta = firstNonEmpty(event.Delta, event.Text, event.Output)
	case "response.function_call_arguments.delta":
		chunk.ToolCallDeltas = []chat.ToolCallDelta{{
			Index: event.Index, ID: firstNonEmpty(event.CallID, event.ItemID),
			Name: event.Name, ArgsDelta: firstNonEmpty(event.Delta, event.Arguments),
		}}
	case "response.function_call_arguments.done":
		chunk.ToolCallDeltas = []chat.ToolCallDelta{{
			Index: event.Index, ID: firstNonEmpty(event.CallID, event.ItemID), Name: event.Name,
		}}
	case "response.output_item.added":
		name, callID, itemID := event.Name, event.CallID, event.ItemID
		if event.Item != nil {
			name = firstNonEmpty(name, event.Item.Name)
			callID = firstNonEmpty(callID, event.Item.CallID)
			itemID = firstNonEmpty(itemID, event.Item.ID)
		}
		if name != "" || callID != "" {
			chunk.ToolCallDeltas = []chat.ToolCallDelta{{
				Index: event.Index, ID: firstNonEmpty(callID, itemID), Name: name,
			}}
		}
	case "response.completed":
		chunk.Done = true
		chunk.FinishReason = "stop"
	}
	if event.Usage != nil {
		usage := event.Usage.toUsage()
		chunk.Usage = &usage
	} else if event.Response != nil && event.Response.Usage != nil {
		usage := event.Response.Usage.toUsage()
		chunk.Usage = &usage
	}
	ok := chunk.Delta != "" || chunk.ReasoningDelta != "" || len(chunk.ToolCallDeltas) > 0 || chunk.Done || chunk.Usage != nil
	return chunk, ok, nil
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// sanitiseResponsesSchema strips keywords that the OpenAI Responses API
// rejects at the top level of a tool's JSON Schema parameters object.
// The Responses API only allows {"type": "object", ...} and rejects
// anyOf, oneOf, allOf, enum, const, and not at the top level.
//
// This is a lossy conversion: it favours safety (the call succeeds)
// over schema strictness. Downstream tool executors are expected to
// validate parameters independently.
func sanitiseResponsesSchema(params json.RawMessage) json.RawMessage {
	if len(params) == 0 || bytes.Equal(params, []byte("null")) {
		return params
	}

	var schema map[string]json.RawMessage
	if err := json.Unmarshal(params, &schema); err != nil {
		// Can't parse — return as-is and let the API reject it.
		return params
	}

	disallowed := []string{"anyOf", "oneOf", "allOf", "enum", "const", "not"}
	cleaned := false
	for _, key := range disallowed {
		if _, exists := schema[key]; exists {
			delete(schema, key)
			cleaned = true
		}
	}

	// Also strip "required" when we removed anyOf/oneOf — the
	// remaining schema is intentionally looser so the API passes
	// validation. Tool executors handle their own validation.
	if cleaned {
		delete(schema, "required")
	}

	result, err := json.Marshal(schema)
	if err != nil {
		return params
	}
	return json.RawMessage(result)
}
