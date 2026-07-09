package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
)

// imagePartToURL converts a chat.ImagePart into a URL string suitable
// for OpenAI's image_url content block. Inline data is base64-encoded
// into a data URI.
func imagePartToURL(p chat.ImagePart) (string, error) {
	if strings.TrimSpace(p.URL) != "" {
		return p.URL, nil
	}
	if len(p.Data) == 0 {
		return "", fmt.Errorf("ImagePart has neither URL nor Data")
	}
	mediaType := strings.TrimSpace(p.MediaType)
	if mediaType == "" {
		return "", fmt.Errorf("ImagePart Data requires MediaType")
	}
	return fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(p.Data)), nil
}

const (
	defaultBaseURL = "https://api.openai.com"
	defaultTimeout = 5 * time.Minute
)

// Config configures an OpenAI Provider.
type Config struct {
	// APIKey is the OpenAI API key. Required.
	APIKey string
	// BaseURL overrides the API base URL. Defaults to https://api.openai.com.
	BaseURL string
	// HTTPClient overrides the HTTP client used for requests.
	HTTPClient *http.Client
}

// Provider is a chat.Provider backed by the OpenAI chat completions API.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// Compile-time assertion that *Provider implements chat.Provider.
var _ chat.Provider = (*Provider)(nil)

// New returns a new OpenAI Provider. APIKey is required for the
// official api.openai.com endpoint, but may be empty for self-hosted
// or local OpenAI-compatible endpoints (e.g. llama.cpp, Ollama).
func New(cfg Config) (*Provider, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	if cfg.APIKey == "" && base == defaultBaseURL {
		return nil, fmt.Errorf("openai: APIKey is required for api.openai.com: %w", chat.ErrInvalidRequest)
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	return &Provider{apiKey: cfg.APIKey, baseURL: normaliseBaseURL(base), client: hc}, nil
}

// normaliseBaseURL canonicalises an OpenAI-compatible base URL to the API root
// that includes the version segment, so callers may pass either a bare host or
// a full versioned base and the endpoints below append only their method path.
//
// A host-only URL (no path) gets the conventional "/v1" appended, preserving
// the historical default of https://api.openai.com → .../v1/chat/completions.
// A URL that already carries a path is taken to be the complete base and is
// left untouched, which lets non-standard layouts work too:
//
//	https://api.openai.com                       → https://api.openai.com/v1
//	https://api.deepseek.com/v1                   → https://api.deepseek.com/v1
//	https://openrouter.ai/api/v1                  → https://openrouter.ai/api/v1
//	https://generativelanguage.googleapis.com/v1beta/openai (unchanged)
func normaliseBaseURL(base string) string {
	base = strings.TrimRight(base, "/")
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	if u.Path == "" || u.Path == "/" {
		return base + "/v1"
	}
	return base
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "openai" }

// --- wire types ----------------------------------------------------------

// wireFunctionCall mirrors the OpenAI function call wire shape. Arguments
// is a JSON-encoded STRING on the wire (not a structured object).
type wireFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type wireToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function wireFunctionCall `json:"function"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`

	// ReasoningContent is DeepSeek's wire field for reasoning/thinking
	// text; Reasoning is Ollama's OpenAI-compat equivalent. The two are
	// mutually exclusive in practice (each server sends only one), so
	// reasoningText() picks whichever is set.
	ReasoningContent string `json:"reasoning_content,omitempty"`
	Reasoning        string `json:"reasoning,omitempty"`
}

// reasoningText returns whichever reasoning field the server populated.
func (m wireMessage) reasoningText() string {
	if m.ReasoningContent != "" {
		return m.ReasoningContent
	}
	return m.Reasoning
}

type wireUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type wireChoice struct {
	Index        int         `json:"index"`
	Message      wireMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type wireResponse struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []wireChoice `json:"choices"`
	Usage   wireUsage    `json:"usage"`
}

// wireDeltaToolCall is a per-chunk tool-call delta. Function is a pointer
// so its absence in a chunk can be distinguished from a zero-valued
// payload (some deltas only carry an arguments fragment).
type wireDeltaToolCall struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function *wireFunctionCall `json:"function,omitempty"`
}

type wireDelta struct {
	Role      string              `json:"role,omitempty"`
	Content   string              `json:"content,omitempty"`
	ToolCalls []wireDeltaToolCall `json:"tool_calls,omitempty"`

	// See wireMessage.ReasoningContent / Reasoning for why there are two.
	ReasoningContent string `json:"reasoning_content,omitempty"`
	Reasoning        string `json:"reasoning,omitempty"`
}

// reasoningText returns whichever reasoning field the server populated.
func (d wireDelta) reasoningText() string {
	if d.ReasoningContent != "" {
		return d.ReasoningContent
	}
	return d.Reasoning
}

type wireStreamChoice struct {
	Index        int       `json:"index"`
	Delta        wireDelta `json:"delta"`
	FinishReason string    `json:"finish_reason,omitempty"`
}

type wireStreamChunk struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Choices []wireStreamChoice `json:"choices"`
	Usage   *wireUsage         `json:"usage,omitempty"`
}

// --- request building ----------------------------------------------------

func (p *Provider) buildBody(req chat.Request, stream bool) (map[string]any, []chat.Warning, error) {
	if req.Model == "" {
		return nil, nil, fmt.Errorf("openai: model is required: %w", chat.ErrInvalidRequest)
	}
	if len(req.Messages) == 0 {
		return nil, nil, fmt.Errorf("openai: at least one message is required: %w", chat.ErrInvalidRequest)
	}
	var warnings []chat.Warning
	msgs := make([]wireMessage, len(req.Messages))
	for i, m := range req.Messages {
		wm := wireMessage{Role: string(m.Role), Name: m.Name}
		// Walk canonical Parts. OpenAI supports either a single
		// content string or an array of content blocks. We use the
		// array form whenever non-text parts are present.
		var textChunks []string
		var contentBlocks []map[string]any
		for _, part := range m.GetParts() {
			switch p := part.(type) {
			case chat.TextPart:
				if p.Text != "" {
					textChunks = append(textChunks, p.Text)
				}
			case chat.ImagePart:
				imageURL, err := imagePartToURL(p)
				if err != nil {
					warnings = append(warnings, chat.Warning{
						Type:    "invalid-content",
						Message: fmt.Sprintf("openai: %v", err),
					})
					continue
				}
				contentBlocks = append(contentBlocks, map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": imageURL,
					},
				})
			case chat.FilePart:
				warnings = append(warnings, chat.Warning{
					Type:    "unsupported-content",
					Message: "openai: FilePart not supported by current models",
				})
			case chat.ReasoningPart:
				// OpenAI does not have a reasoning_content wire field
				// like DeepSeek. Append reasoning text to content for
				// non-assistant turns; for assistant turns warn and
				// drop (OpenAI has no mechanism to re-inject reasoning
				// history).
				if m.Role == chat.RoleAssistant {
					warnings = append(warnings, chat.Warning{
						Type:    "unsupported-content",
						Message: "openai: ReasoningPart on assistant input dropped; OpenAI has no reasoning replay mechanism",
					})
				} else if p.Text != "" {
					textChunks = append(textChunks, p.Text)
				}
			}
		}
		switch {
		case len(contentBlocks) > 0:
			// Structured array content. Add text as a text block if present.
			if len(textChunks) > 0 {
				contentBlocks = append([]map[string]any{{"type": "text", "text": strings.Join(textChunks, "")}}, contentBlocks...)
			}
			wm.Content = contentBlocks
		case len(textChunks) > 0:
			wm.Content = strings.Join(textChunks, "")
		}
		switch m.Role {
		case chat.RoleAssistant:
			if len(m.ToolCalls) > 0 {
				tcs := make([]wireToolCall, len(m.ToolCalls))
				for j, tc := range m.ToolCalls {
					id := tc.ID
					if id == "" {
						id = fmt.Sprintf("call_%d", j)
					}
					tcs[j] = wireToolCall{
						ID:   id,
						Type: "function",
						Function: wireFunctionCall{
							Name:      tc.Name,
							Arguments: tc.Arguments,
						},
					}
				}
				wm.ToolCalls = tcs
			}
		case chat.RoleTool:
			if m.ToolCallID == "" {
				return nil, nil, fmt.Errorf("openai: tool message at index %d missing ToolCallID: %w", i, chat.ErrInvalidRequest)
			}
			wm.ToolCallID = m.ToolCallID
		}
		msgs[i] = wm
	}
	body := map[string]any{
		"model":    req.Model,
		"messages": msgs,
		"stream":   stream,
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			fn := map[string]any{"name": t.Name}
			if t.Description != "" {
				fn["description"] = t.Description
			}
			if len(t.Parameters) > 0 {
				fn["parameters"] = t.Parameters
			}
			tools[i] = map[string]any{
				"type":     "function",
				"function": fn,
			}
		}
		body["tools"] = tools
	}
	if req.ToolChoice != nil {
		switch req.ToolChoice.Type {
		case chat.ToolChoiceAuto:
			body["tool_choice"] = "auto"
		case chat.ToolChoiceNone:
			body["tool_choice"] = "none"
		case chat.ToolChoiceRequired:
			body["tool_choice"] = "required"
		case chat.ToolChoiceTool:
			if req.ToolChoice.Name == "" {
				return nil, nil, fmt.Errorf("openai: tool_choice type=tool requires Name: %w", chat.ErrInvalidRequest)
			}
			body["tool_choice"] = map[string]any{
				"type":     "function",
				"function": map[string]any{"name": req.ToolChoice.Name},
			}
		default:
			return nil, nil, fmt.Errorf("openai: unknown tool_choice type %q: %w", req.ToolChoice.Type, chat.ErrInvalidRequest)
		}
	}
	// Apply provider-specific options.
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

func (p *Provider) newHTTPRequest(ctx context.Context, body map[string]any) (*http.Request, error) {
	path := "/chat/completions"
	hasTools := false
	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		hasTools = true
	} else if tools, ok := body["tools"].([]map[string]any); ok && len(tools) > 0 {
		hasTools = true
	} else if body["tools"] != nil {
		hasTools = true
	}
	reasoningEffort, _ := body["reasoning_effort"].(string)
	if hasTools && reasoningEffort != "" && reasoningEffort != "none" {
		path = "/responses"
	}

	if path == "/responses" {
		if msgs, ok := body["messages"]; ok {
			body["input"] = msgs
			delete(body, "messages")
		}
		delete(body, "stream_options")
		// The Responses API rejects the flat Chat Completions field and
		// expects reasoning effort nested under "reasoning" instead.
		if reasoningEffort != "" {
			body["reasoning"] = map[string]any{"effort": reasoningEffort}
			delete(body, "reasoning_effort")
		}
		// The Responses API renames max_tokens to max_output_tokens.
		if maxTokens, ok := body["max_tokens"]; ok {
			body["max_output_tokens"] = maxTokens
			delete(body, "max_tokens")
		}
		// The Responses API has no equivalent of Chat Completions' "stop"
		// sequences, and reasoning models reject sampling params
		// (temperature/top_p) via /responses since reasoning uses fixed
		// sampling. Unlike max_tokens/reasoning_effort these aren't
		// renames, they're just unsupported here, so drop rather than
		// translate.
		delete(body, "stop")
		delete(body, "temperature")
		delete(body, "top_p")
		// The Responses API flattens tool/tool_choice function shape:
		// {"type":"function","name":...,"description":...,"parameters":...}
		// instead of Chat Completions' nested {"type":"function","function":{...}}.
		if tools, ok := body["tools"].([]map[string]any); ok {
			for _, t := range tools {
				fn, ok := t["function"].(map[string]any)
				if !ok {
					continue
				}
				delete(t, "function")
				maps.Copy(t, fn)
			}
		}
		if tc, ok := body["tool_choice"].(map[string]any); ok {
			if fn, ok := tc["function"].(map[string]any); ok {
				delete(tc, "function")
				maps.Copy(tc, fn)
			}
		}
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	// Inject extra headers from the request context, e.g. from plugin hooks.
	if headers, ok := chat.ContextHeaders(ctx); ok {
		for k, v := range headers {
			httpReq.Header.Set(k, v)
		}
	}
	return httpReq, nil
}

// classifyHTTPError maps an HTTP error response to a wrapped sentinel error.
func classifyHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	snippet := strings.TrimSpace(string(body))
	var base error
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		base = chat.ErrAuthFailed
	case resp.StatusCode == http.StatusTooManyRequests:
		base = chat.ErrRateLimited
	case resp.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(snippet), "context length"):
		base = chat.ErrContextLength
	case resp.StatusCode == http.StatusBadRequest:
		base = chat.ErrInvalidRequest
	case resp.StatusCode >= 500:
		base = chat.ErrProviderUnavailable
	default:
		base = chat.ErrProviderUnavailable
	}
	return fmt.Errorf("openai: status %d: %s: %w", resp.StatusCode, snippet, base)
}

// --- Chat (non-streaming) ------------------------------------------------

// Chat performs a non-streaming chat completion.
func (p *Provider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	body, warnings, err := p.buildBody(req, false)
	if err != nil {
		return chat.Response{}, err
	}
	httpReq, err := p.newHTTPRequest(ctx, body)
	if err != nil {
		return chat.Response{}, err
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return chat.Response{}, fmt.Errorf("openai: http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return chat.Response{}, classifyHTTPError(resp)
	}
	isResponses := strings.HasSuffix(httpReq.URL.Path, "/responses")
	if isResponses {
		var wr struct {
			ID     string `json:"id"`
			Model  string `json:"model"`
			Output []struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Type      string `json:"type"`
					Text      string `json:"text"`
					CallID    string `json:"call_id"`
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"content"`
				Summary string `json:"summary"`
				// A tool call is its own top-level output item
				// (type: "function_call") with these fields set directly
				// on the item, not nested inside Content like a message's
				// text/tool blocks.
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"output"`
			Usage struct {
				InputTokens      int `json:"input_tokens"`
				OutputTokens     int `json:"output_tokens"`
				TotalTokens      int `json:"total_tokens"`
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
			return chat.Response{}, fmt.Errorf("openai: decode responses api response: %w", err)
		}
		out := chat.Response{
			ID:       wr.ID,
			Model:    wr.Model,
			Role:     chat.RoleAssistant,
			Warnings: warnings,
			Usage: chat.Usage{
				PromptTokens:     firstNonZero(wr.Usage.InputTokens, wr.Usage.PromptTokens),
				CompletionTokens: firstNonZero(wr.Usage.OutputTokens, wr.Usage.CompletionTokens),
				TotalTokens:      firstNonZero(wr.Usage.TotalTokens),
			},
		}
		for _, item := range wr.Output {
			if item.Type == "reasoning" && item.Summary != "" {
				out.Parts = append(out.Parts, chat.ReasoningPart{Text: item.Summary})
			} else if item.Type == "function_call" {
				// The Responses API emits each tool call as its own
				// top-level output item, not nested inside a "message".
				out.ToolCalls = append(out.ToolCalls, chat.ToolCall{
					ID:        item.CallID,
					Name:      item.Name,
					Arguments: item.Arguments,
				})
			} else if item.Type == "message" {
				if item.Role != "" {
					out.Role = chat.Role(item.Role)
				}
				for _, block := range item.Content {
					switch block.Type {
					case "output_text", "text":
						out.Content += block.Text
						out.Parts = append(out.Parts, chat.TextPart{Text: block.Text})
					case "function_call", "tool_call":
						out.ToolCalls = append(out.ToolCalls, chat.ToolCall{
							ID:        block.CallID,
							Name:      block.Name,
							Arguments: block.Arguments,
						})
					}
				}
			}
		}
		if len(out.ToolCalls) > 0 && out.FinishReason == "" {
			out.FinishReason = "tool_calls"
		}
		return out, nil
	}

	var wr wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return chat.Response{}, fmt.Errorf("openai: decode response: %w", err)
	}
	out := chat.Response{
		ID:       wr.ID,
		Model:    wr.Model,
		Role:     chat.RoleAssistant,
		Warnings: warnings,
		Usage: chat.Usage{
			PromptTokens:     wr.Usage.PromptTokens,
			CompletionTokens: wr.Usage.CompletionTokens,
			TotalTokens:      wr.Usage.TotalTokens,
		},
	}
	if len(wr.Choices) > 0 {
		c := wr.Choices[0]
		contentText := extractTextContent(c.Message.Content)
		out.Content = contentText
		out.FinishReason = c.FinishReason
		if c.Message.Role != "" {
			out.Role = chat.Role(c.Message.Role)
		}
		// Populate canonical Parts: ReasoningPart first (if any), then TextPart.
		if reasoning := c.Message.reasoningText(); reasoning != "" {
			out.Parts = append(out.Parts, chat.ReasoningPart{Text: reasoning})
		}
		if contentText != "" {
			out.Parts = append(out.Parts, chat.TextPart{Text: contentText})
		}
		if len(c.Message.ToolCalls) > 0 {
			tcs := make([]chat.ToolCall, len(c.Message.ToolCalls))
			for i, tc := range c.Message.ToolCalls {
				tcs[i] = chat.ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				}
			}
			out.ToolCalls = tcs
			if out.FinishReason == "" {
				out.FinishReason = "tool_calls"
			}
		}
	}
	return out, nil
}

// --- provider-specific options ------------------------------------------

// openaiProviderOptions holds provider-specific options for OpenAI.
type openaiProviderOptions struct {
	// ReasoningEffort controls how much internal reasoning the model
	// performs before responding. Supported values: "none", "minimal",
	// "low", "medium", "high", "xhigh".
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// ReasoningSummary controls whether reasoning content includes
	// summaries. Supported values: "auto", "detailed".
	ReasoningSummary string `json:"reasoning_summary,omitempty"`
}

// extractTextContent returns the plain text from a wire content field
// that may be either a string or a content-block array.
func extractTextContent(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var out []string
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if typ, _ := m["type"].(string); typ == "text" {
				if t, ok := m["text"].(string); ok {
					out = append(out, t)
				}
			}
		}
		return strings.Join(out, "")
	default:
		return ""
	}
}

// --- ChatStream ----------------------------------------------------------

// ChatStream performs a streaming chat completion. Callers must Close the
// returned Stream when finished.
func (p *Provider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	body, warnings, err := p.buildBody(req, true)
	if err != nil {
		return nil, err
	}
	httpReq, err := p.newHTTPRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: http do: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := classifyHTTPError(resp)
		resp.Body.Close()
		return nil, err
	}
	isResponses := strings.HasSuffix(httpReq.URL.Path, "/responses")
	return &stream{
		resp:            resp,
		reader:          bufio.NewReader(resp.Body),
		pendingWarnings: warnings,
		isResponses:     isResponses,
	}, nil
}

type stream struct {
	resp            *http.Response
	reader          *bufio.Reader
	closed          bool
	finished        bool
	doneEmitted     bool
	isResponses     bool
	pendingUsage    *chat.Usage
	pendingWarnings []chat.Warning
	// bufferedFinal holds a Done=true chunk that was deferred when a
	// finish_reason was observed, so that the trailing usage-only chunk
	// (which OpenAI emits AFTER finish_reason when stream_options.
	// include_usage=true) can be attached before emission.
	bufferedFinal *chat.Chunk
}

// Next returns the next chunk in the stream.
func (s *stream) Next(ctx context.Context) (chat.Chunk, error) {
	if err := ctx.Err(); err != nil {
		return chat.Chunk{}, err
	}
	if s.closed {
		return chat.Chunk{}, io.EOF
	}
	if s.finished {
		return chat.Chunk{}, io.EOF
	}
	for {
		if err := ctx.Err(); err != nil {
			return chat.Chunk{}, err
		}
		line, err := s.reader.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if errors.Is(err, io.EOF) {
				s.finished = true
				if s.bufferedFinal != nil {
					out := *s.bufferedFinal
					s.bufferedFinal = nil
					if out.Usage == nil {
						out.Usage = s.pendingUsage
					}
					s.doneEmitted = true
					return out, nil
				}
				if !s.doneEmitted {
					s.doneEmitted = true
					return chat.Chunk{Done: true, Usage: s.pendingUsage}, nil
				}
				return chat.Chunk{}, io.EOF
			}
			return chat.Chunk{}, fmt.Errorf("openai: stream read: %w", err)
		}
		trimmed := bytes.TrimRight(line, "\r\n")
		if len(trimmed) == 0 {
			if errors.Is(err, io.EOF) {
				s.finished = true
				if s.bufferedFinal != nil {
					out := *s.bufferedFinal
					s.bufferedFinal = nil
					if out.Usage == nil {
						out.Usage = s.pendingUsage
					}
					s.doneEmitted = true
					return out, nil
				}
				if !s.doneEmitted {
					s.doneEmitted = true
					return chat.Chunk{Done: true, Usage: s.pendingUsage}, nil
				}
				return chat.Chunk{}, io.EOF
			}
			continue
		}
		if trimmed[0] == ':' {
			continue
		}
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(trimmed[len("data:"):])
		if len(data) == 0 {
			continue
		}
		if bytes.Equal(data, []byte("[DONE]")) {
			s.finished = true
			if s.bufferedFinal != nil {
				out := *s.bufferedFinal
				s.bufferedFinal = nil
				if out.Usage == nil {
					out.Usage = s.pendingUsage
				}
				s.doneEmitted = true
				return out, nil
			}
			if !s.doneEmitted {
				s.doneEmitted = true
				return chat.Chunk{Done: true, Usage: s.pendingUsage}, nil
			}
			return chat.Chunk{}, io.EOF
		}
		if s.isResponses {
			chunk, ok := parseResponsesSSEChunk(data)
			if ok {
				if chunk.Done {
					s.finished = true
					if chunk.Usage == nil {
						chunk.Usage = s.pendingUsage
					}
					s.doneEmitted = true
				} else if chunk.Usage != nil {
					s.pendingUsage = chunk.Usage
				}
				if len(s.pendingWarnings) > 0 {
					chunk.Warnings = s.pendingWarnings
					s.pendingWarnings = nil
				}
				return chunk, nil
			}
			continue
		}
		var ch wireStreamChunk
		if jerr := json.Unmarshal(data, &ch); jerr != nil {
			return chat.Chunk{}, fmt.Errorf("openai: decode stream chunk: %w", jerr)
		}
		if ch.Usage != nil {
			s.pendingUsage = &chat.Usage{
				PromptTokens:     ch.Usage.PromptTokens,
				CompletionTokens: ch.Usage.CompletionTokens,
				TotalTokens:      ch.Usage.TotalTokens,
			}
		}
		if len(ch.Choices) == 0 {
			continue
		}
		c := ch.Choices[0]
		role := chat.Role(c.Delta.Role)
		if role == "" {
			role = chat.RoleAssistant
		}
		var tcDeltas []chat.ToolCallDelta
		if len(c.Delta.ToolCalls) > 0 {
			tcDeltas = make([]chat.ToolCallDelta, 0, len(c.Delta.ToolCalls))
			for _, tc := range c.Delta.ToolCalls {
				d := chat.ToolCallDelta{Index: tc.Index, ID: tc.ID}
				if tc.Function != nil {
					d.Name = tc.Function.Name
					d.ArgsDelta = tc.Function.Arguments
				}
				tcDeltas = append(tcDeltas, d)
			}
		}
		if c.FinishReason != "" {
			// Defer emission so we can attach trailing usage.
			final := chat.Chunk{
				Done:         true,
				Role:         role,
				FinishReason: c.FinishReason,
				Usage:        s.pendingUsage,
			}
			s.bufferedFinal = &final
			// If this same chunk also carried delta content, reasoning,
			// or tool-call deltas, emit those now and let the buffered
			// final go out on the next call.
			if c.Delta.Content != "" || c.Delta.reasoningText() != "" || len(tcDeltas) > 0 {
				out := chat.Chunk{
					Delta:          c.Delta.Content,
					ReasoningDelta: c.Delta.reasoningText(),
					Role:           role,
					ToolCallDeltas: tcDeltas,
				}
				if len(s.pendingWarnings) > 0 {
					out.Warnings = s.pendingWarnings
					s.pendingWarnings = nil
				}
				return out, nil
			}
			continue
		}
		out := chat.Chunk{
			Delta:          c.Delta.Content,
			ReasoningDelta: c.Delta.reasoningText(),
			Role:           role,
			ToolCallDeltas: tcDeltas,
		}
		if len(s.pendingWarnings) > 0 {
			out.Warnings = s.pendingWarnings
			s.pendingWarnings = nil
		}
		return out, nil
	}
}

// Close releases resources associated with the stream.
func (s *stream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.resp != nil && s.resp.Body != nil {
		return s.resp.Body.Close()
	}
	return nil
}

func parseResponsesSSEChunk(data []byte) (chat.Chunk, bool) {
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
		Usage     *struct {
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
			TotalTokens      int `json:"total_tokens"`
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Response *struct {
			Usage *struct {
				InputTokens      int `json:"input_tokens"`
				OutputTokens     int `json:"output_tokens"`
				TotalTokens      int `json:"total_tokens"`
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return chat.Chunk{}, false
	}
	chunk := chat.Chunk{}
	switch event.Type {
	case "response.output_text.delta", "response.text.delta":
		chunk.Delta = firstNonEmpty(event.Delta, event.Text, event.Output)
	case "response.reasoning_text.delta", "response.reasoning.delta":
		chunk.ReasoningDelta = firstNonEmpty(event.Delta, event.Text, event.Output)
	case "response.function_call_arguments.delta":
		chunk.ToolCallDeltas = []chat.ToolCallDelta{{
			Index:     event.Index,
			ID:        firstNonEmpty(event.CallID, event.ItemID),
			Name:      event.Name,
			ArgsDelta: firstNonEmpty(event.Delta, event.Arguments),
		}}
	case "response.output_item.added":
		if event.Name != "" || event.CallID != "" {
			chunk.ToolCallDeltas = []chat.ToolCallDelta{{
				Index: event.Index,
				ID:    firstNonEmpty(event.CallID, event.ItemID),
				Name:  event.Name,
			}}
		}
	case "response.completed":
		chunk.Done = true
		chunk.FinishReason = "stop"
	}
	if event.Usage != nil {
		chunk.Usage = &chat.Usage{
			PromptTokens:     firstNonZero(event.Usage.InputTokens, event.Usage.PromptTokens),
			CompletionTokens: firstNonZero(event.Usage.OutputTokens, event.Usage.CompletionTokens),
			TotalTokens:      firstNonZero(event.Usage.TotalTokens),
		}
	}
	if chunk.Usage == nil && event.Response != nil && event.Response.Usage != nil {
		chunk.Usage = &chat.Usage{
			PromptTokens:     firstNonZero(event.Response.Usage.InputTokens, event.Response.Usage.PromptTokens),
			CompletionTokens: firstNonZero(event.Response.Usage.OutputTokens, event.Response.Usage.CompletionTokens),
			TotalTokens:      firstNonZero(event.Response.Usage.TotalTokens),
		}
	}
	return chunk, chunk.Delta != "" || chunk.ReasoningDelta != "" || len(chunk.ToolCallDeltas) > 0 || chunk.Done || chunk.Usage != nil
}

func firstNonZero(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
