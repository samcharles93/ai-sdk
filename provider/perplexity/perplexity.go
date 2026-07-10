package perplexity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
)

const (
	defaultBaseURL = "https://api.perplexity.ai"
	defaultTimeout = 5 * time.Minute
)

// Config configures a Perplexity Provider.
type Config struct {
	// APIKey is the Perplexity API key. Required.
	APIKey string
	// BaseURL overrides the API base URL. Defaults to https://api.perplexity.ai.
	BaseURL string
	// HTTPClient overrides the HTTP client used for requests.
	HTTPClient *http.Client
}

// Provider is a chat.Provider backed by the Perplexity chat completions API.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// Compile-time assertion that *Provider implements chat.Provider.
var _ chat.Provider = (*Provider)(nil)

// New returns a new Perplexity Provider. It returns an error if APIKey is empty.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("perplexity: APIKey is required: %w", chat.ErrInvalidRequest)
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	return &Provider{apiKey: cfg.APIKey, baseURL: base, client: hc}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "perplexity" }

// --- wire types ----------------------------------------------------------

// wireFunctionCall mirrors the Perplexity function call wire shape. Arguments
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
	Content    string         `json:"content"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
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
	ID        string       `json:"id"`
	Model     string       `json:"model"`
	Choices   []wireChoice `json:"choices"`
	Usage     wireUsage    `json:"usage"`
	Citations []string     `json:"citations,omitempty"`
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
		return nil, nil, fmt.Errorf("perplexity: model is required: %w", chat.ErrInvalidRequest)
	}
	if len(req.Messages) == 0 {
		return nil, nil, fmt.Errorf("perplexity: at least one message is required: %w", chat.ErrInvalidRequest)
	}
	var warnings []chat.Warning
	msgs := make([]wireMessage, len(req.Messages))
	for i, m := range req.Messages {
		wm := wireMessage{Role: string(m.Role), Name: m.Name}
		// Walk canonical Parts, joining text and warning on non-text
		// content types that Perplexity does not support as input.
		var textChunks []string
		for _, part := range m.GetParts() {
			switch p := part.(type) {
			case chat.TextPart:
				if p.Text != "" {
					textChunks = append(textChunks, p.Text)
				}
			case chat.ImagePart:
				warnings = append(warnings, chat.Warning{
					Type:    "unsupported-content",
					Message: "perplexity: ImagePart not supported by current models",
				})
			case chat.FilePart:
				warnings = append(warnings, chat.Warning{
					Type:    "unsupported-content",
					Message: "perplexity: FilePart not supported by current models",
				})
			case chat.ReasoningPart:
				// Perplexity does not have a reasoning_content wire field.
				// Append reasoning text to content for non-assistant turns;
				// for assistant turns warn and drop.
				if m.Role == chat.RoleAssistant {
					warnings = append(warnings, chat.Warning{
						Type:    "unsupported-content",
						Message: "perplexity: ReasoningPart on assistant input dropped; Perplexity has no reasoning replay mechanism",
					})
				} else if p.Text != "" {
					textChunks = append(textChunks, p.Text)
				}
			}
		}
		if len(textChunks) > 0 {
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
				return nil, nil, fmt.Errorf("perplexity: tool message at index %d missing ToolCallID: %w", i, chat.ErrInvalidRequest)
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
				return nil, nil, fmt.Errorf("perplexity: tool_choice type=tool requires Name: %w", chat.ErrInvalidRequest)
			}
			body["tool_choice"] = map[string]any{
				"type":     "function",
				"function": map[string]any{"name": req.ToolChoice.Name},
			}
		default:
			return nil, nil, fmt.Errorf("perplexity: unknown tool_choice type %q: %w", req.ToolChoice.Type, chat.ErrInvalidRequest)
		}
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
	// Perplexity-specific: search_recency_filter
	if v, ok := req.Metadata["search_recency_filter"]; ok && v != "" {
		body["search_recency_filter"] = v
	}
	// Perplexity-specific: return_images
	if v, ok := req.Metadata["return_images"]; ok {
		body["return_images"] = v == "true"
	}
	return body, warnings, nil
}

func (p *Provider) newHTTPRequest(ctx context.Context, body map[string]any) (*http.Request, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("perplexity: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("perplexity: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	return httpReq, nil
}

// classifyHTTPError maps an HTTP error response to a wrapped sentinel error.
func classifyHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	snippet := chat.SanitizeErrorBody(body)
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
	return fmt.Errorf("perplexity: status %d: %s: %w", resp.StatusCode, snippet, base)
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
		return chat.Response{}, fmt.Errorf("perplexity: http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return chat.Response{}, classifyHTTPError(resp)
	}
	var wr wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return chat.Response{}, fmt.Errorf("perplexity: decode response: %w", err)
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
	// Perplexity-specific: include citations in ProviderMetadata.
	if len(wr.Citations) > 0 {
		if out.ProviderMetadata == nil {
			out.ProviderMetadata = make(map[string]any)
		}
		out.ProviderMetadata["perplexity:citations"] = wr.Citations
	}
	if len(wr.Choices) > 0 {
		c := wr.Choices[0]
		out.Content = c.Message.Content
		out.FinishReason = c.FinishReason
		if c.Message.Role != "" {
			out.Role = chat.Role(c.Message.Role)
		}
		// Populate canonical Parts: TextPart from content.
		if c.Message.Content != "" {
			out.Parts = append(out.Parts, chat.TextPart{Text: c.Message.Content})
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
		return nil, fmt.Errorf("perplexity: http do: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := classifyHTTPError(resp)
		resp.Body.Close()
		return nil, err
	}
	return &stream{
		resp:            resp,
		reader:          bufio.NewReader(resp.Body),
		pendingWarnings: warnings,
	}, nil
}

type stream struct {
	resp            *http.Response
	reader          *bufio.Reader
	closed          bool
	finished        bool
	doneEmitted     bool
	pendingUsage    *chat.Usage
	pendingWarnings []chat.Warning
	bufferedFinal   *chat.Chunk
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
			return chat.Chunk{}, fmt.Errorf("perplexity: stream read: %w", err)
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
		var ch wireStreamChunk
		if jerr := json.Unmarshal(data, &ch); jerr != nil {
			return chat.Chunk{}, fmt.Errorf("perplexity: decode stream chunk: %w", jerr)
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
			// If this same chunk also carried delta content or tool-call
			// deltas, emit those now and let the buffered final go out on
			// the next call.
			if c.Delta.Content != "" || len(tcDeltas) > 0 {
				out := chat.Chunk{
					Delta:          c.Delta.Content,
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
