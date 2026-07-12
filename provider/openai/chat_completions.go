package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/samcharles93/ai-sdk/chat"
)

type chatCompletionsAPI struct{}

func (chatCompletionsAPI) path() string { return "/chat/completions" }

type chatCompletionsFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type chatCompletionsToolCall struct {
	ID       string                      `json:"id,omitempty"`
	Type     string                      `json:"type,omitempty"`
	Function chatCompletionsFunctionCall `json:"function"`
}

type chatCompletionsMessage struct {
	Role       string                    `json:"role"`
	Content    any                       `json:"content"`
	Name       string                    `json:"name,omitempty"`
	ToolCalls  []chatCompletionsToolCall `json:"tool_calls,omitempty"`
	ToolCallID string                    `json:"tool_call_id,omitempty"`

	ReasoningContent string `json:"reasoning_content,omitempty"`
	Reasoning        string `json:"reasoning,omitempty"`
}

func (m chatCompletionsMessage) reasoningText() string {
	if m.ReasoningContent != "" {
		return m.ReasoningContent
	}
	return m.Reasoning
}

type chatCompletionsUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

func (u chatCompletionsUsage) cachedTokens() int {
	if u.PromptTokensDetails == nil {
		return 0
	}
	return u.PromptTokensDetails.CachedTokens
}

type chatCompletionsChoice struct {
	Index        int                    `json:"index"`
	Message      chatCompletionsMessage `json:"message"`
	FinishReason string                 `json:"finish_reason"`
}

type chatCompletionsResponse struct {
	ID      string                  `json:"id"`
	Model   string                  `json:"model"`
	Choices []chatCompletionsChoice `json:"choices"`
	Usage   chatCompletionsUsage    `json:"usage"`
}

type chatCompletionsDeltaToolCall struct {
	Index    int                          `json:"index"`
	ID       string                       `json:"id,omitempty"`
	Type     string                       `json:"type,omitempty"`
	Function *chatCompletionsFunctionCall `json:"function,omitempty"`
}

type chatCompletionsDelta struct {
	Role             string                         `json:"role,omitempty"`
	Content          string                         `json:"content,omitempty"`
	ToolCalls        []chatCompletionsDeltaToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string                         `json:"reasoning_content,omitempty"`
	Reasoning        string                         `json:"reasoning,omitempty"`
}

func (d chatCompletionsDelta) reasoningText() string {
	if d.ReasoningContent != "" {
		return d.ReasoningContent
	}
	return d.Reasoning
}

type chatCompletionsStreamChoice struct {
	Index        int                  `json:"index"`
	Delta        chatCompletionsDelta `json:"delta"`
	FinishReason string               `json:"finish_reason,omitempty"`
}

type chatCompletionsStreamChunk struct {
	ID      string                        `json:"id"`
	Model   string                        `json:"model"`
	Choices []chatCompletionsStreamChoice `json:"choices"`
	Usage   *chatCompletionsUsage         `json:"usage,omitempty"`
}

func (chatCompletionsAPI) buildBody(req chat.Request, stream bool) (map[string]any, []chat.Warning, error) {
	messages, warnings, err := buildChatCompletionsMessages(req.Messages)
	if err != nil {
		return nil, nil, err
	}
	body := map[string]any{"model": req.Model, "messages": messages, "stream": stream}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, tool := range req.Tools {
			function := map[string]any{"name": tool.Name}
			if tool.Description != "" {
				function["description"] = tool.Description
			}
			if len(tool.Parameters) > 0 {
				function["parameters"] = tool.Parameters
			}
			tools[i] = map[string]any{"type": "function", "function": function}
		}
		body["tools"] = tools
	}
	if err := applyChatCompletionsToolChoice(body, req.ToolChoice); err != nil {
		return nil, nil, err
	}
	opts, _ := chat.ProviderOptionsFor[openaiProviderOptions](req.ProviderOptions, "openai")
	if opts.ReasoningEffort != "" {
		body["reasoning_effort"] = opts.ReasoningEffort
	}
	if req.Temperature != 0 {
		body["temperature"] = req.Temperature
	}
	if req.MaxTokens != 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.TopP != 0 {
		body["top_p"] = req.TopP
	}
	if len(req.Stop) > 0 {
		body["stop"] = req.Stop
	}
	if stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	return body, warnings, nil
}

func buildChatCompletionsMessages(messages []chat.Message) ([]chatCompletionsMessage, []chat.Warning, error) {
	wireMessages := make([]chatCompletionsMessage, len(messages))
	var warnings []chat.Warning
	for i, message := range messages {
		wireMessage := chatCompletionsMessage{Role: string(message.Role), Name: message.Name}
		var textChunks []string
		var contentBlocks []map[string]any
		for _, part := range message.GetParts() {
			switch part := part.(type) {
			case chat.TextPart:
				if part.Text != "" {
					textChunks = append(textChunks, part.Text)
				}
			case chat.ImagePart:
				imageURL, err := imagePartToURL(part)
				if err != nil {
					warnings = append(warnings, chat.Warning{Type: "invalid-content", Message: fmt.Sprintf("openai: %v", err)})
					continue
				}
				contentBlocks = append(contentBlocks, map[string]any{
					"type": "image_url", "image_url": map[string]any{"url": imageURL},
				})
			case chat.FilePart:
				warnings = append(warnings, chat.Warning{Type: "unsupported-content", Message: "openai: FilePart not supported by current models"})
			case chat.ReasoningPart:
				if message.Role == chat.RoleAssistant {
					warnings = append(warnings, chat.Warning{Type: "unsupported-content", Message: "openai: ReasoningPart on assistant input dropped; OpenAI has no reasoning replay mechanism"})
				} else if part.Text != "" {
					textChunks = append(textChunks, part.Text)
				}
			}
		}
		switch {
		case len(contentBlocks) > 0:
			if len(textChunks) > 0 {
				contentBlocks = append([]map[string]any{{"type": "text", "text": strings.Join(textChunks, "")}}, contentBlocks...)
			}
			wireMessage.Content = contentBlocks
		case len(textChunks) > 0:
			wireMessage.Content = strings.Join(textChunks, "")
		}
		switch message.Role {
		case chat.RoleAssistant:
			if len(message.ToolCalls) > 0 {
				wireMessage.ToolCalls = make([]chatCompletionsToolCall, len(message.ToolCalls))
				for j, toolCall := range message.ToolCalls {
					id := toolCall.ID
					if id == "" {
						id = fmt.Sprintf("call_%d", j)
					}
					wireMessage.ToolCalls[j] = chatCompletionsToolCall{
						ID: id, Type: "function",
						Function: chatCompletionsFunctionCall{Name: toolCall.Name, Arguments: toolCall.Arguments},
					}
				}
			}
		case chat.RoleTool:
			if message.ToolCallID == "" {
				return nil, nil, fmt.Errorf("openai: tool message at index %d missing ToolCallID: %w", i, chat.ErrInvalidRequest)
			}
			wireMessage.ToolCallID = message.ToolCallID
		}
		wireMessages[i] = wireMessage
	}
	return wireMessages, warnings, nil
}

func applyChatCompletionsToolChoice(body map[string]any, choice *chat.ToolChoice) error {
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
		body["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": choice.Name}}
	default:
		return fmt.Errorf("openai: unknown tool_choice type %q: %w", choice.Type, chat.ErrInvalidRequest)
	}
	return nil
}

func (chatCompletionsAPI) decodeResponse(reader io.Reader, warnings []chat.Warning) (chat.Response, error) {
	var wireResponse chatCompletionsResponse
	if err := json.NewDecoder(reader).Decode(&wireResponse); err != nil {
		return chat.Response{}, fmt.Errorf("openai: decode chat completions response: %w", err)
	}
	response := chat.Response{
		ID: wireResponse.ID, Model: wireResponse.Model, Role: chat.RoleAssistant, Warnings: warnings,
		Usage: chat.Usage{
			PromptTokens: wireResponse.Usage.PromptTokens, CompletionTokens: wireResponse.Usage.CompletionTokens,
			TotalTokens: wireResponse.Usage.TotalTokens, CachedTokens: wireResponse.Usage.cachedTokens(),
		},
	}
	if len(wireResponse.Choices) == 0 {
		return response, nil
	}
	choice := wireResponse.Choices[0]
	response.Content = extractTextContent(choice.Message.Content)
	response.FinishReason = choice.FinishReason
	if choice.Message.Role != "" {
		response.Role = chat.Role(choice.Message.Role)
	}
	if reasoning := choice.Message.reasoningText(); reasoning != "" {
		response.Parts = append(response.Parts, chat.ReasoningPart{Text: reasoning})
	}
	if response.Content != "" {
		response.Parts = append(response.Parts, chat.TextPart{Text: response.Content})
	}
	for _, toolCall := range choice.Message.ToolCalls {
		response.ToolCalls = append(response.ToolCalls, chat.ToolCall{
			ID: toolCall.ID, Name: toolCall.Function.Name, Arguments: toolCall.Function.Arguments,
		})
	}
	if len(response.ToolCalls) > 0 && response.FinishReason == "" {
		response.FinishReason = "tool_calls"
	}
	return response, nil
}

func (chatCompletionsAPI) parseStreamEvent(data []byte) (chat.Chunk, bool, error) {
	var wireChunk chatCompletionsStreamChunk
	if err := json.Unmarshal(data, &wireChunk); err != nil {
		return chat.Chunk{}, false, fmt.Errorf("openai: decode chat completions stream chunk: %w", err)
	}
	chunk := chat.Chunk{}
	if wireChunk.Usage != nil {
		chunk.Usage = &chat.Usage{
			PromptTokens: wireChunk.Usage.PromptTokens, CompletionTokens: wireChunk.Usage.CompletionTokens,
			TotalTokens: wireChunk.Usage.TotalTokens, CachedTokens: wireChunk.Usage.cachedTokens(),
		}
	}
	if len(wireChunk.Choices) == 0 {
		return chunk, chunk.Usage != nil, nil
	}
	choice := wireChunk.Choices[0]
	chunk.Role = chat.Role(choice.Delta.Role)
	if chunk.Role == "" {
		chunk.Role = chat.RoleAssistant
	}
	chunk.Delta = choice.Delta.Content
	chunk.ReasoningDelta = choice.Delta.reasoningText()
	chunk.FinishReason = choice.FinishReason
	chunk.Done = choice.FinishReason != ""
	for _, toolCall := range choice.Delta.ToolCalls {
		delta := chat.ToolCallDelta{Index: toolCall.Index, ID: toolCall.ID}
		if toolCall.Function != nil {
			delta.Name = toolCall.Function.Name
			delta.ArgsDelta = toolCall.Function.Arguments
		}
		chunk.ToolCallDeltas = append(chunk.ToolCallDeltas, delta)
	}
	return chunk, true, nil
}

func extractTextContent(value any) string {
	switch content := value.(type) {
	case string:
		return content
	case []any:
		var output []string
		for _, item := range content {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if blockType, _ := block["type"].(string); blockType == "text" {
				if text, ok := block["text"].(string); ok {
					output = append(output, text)
				}
			}
		}
		return strings.Join(output, "")
	default:
		return ""
	}
}
