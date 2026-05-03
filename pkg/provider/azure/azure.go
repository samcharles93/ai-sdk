// Package azure implements chat.Provider, embed.Provider, and
// image.Provider for the Azure OpenAI Service.
//
// Azure OpenAI exposes OpenAI-compatible endpoints under a
// resource-specific URL with api-key authentication. This package
// speaks the OpenAI wire format, adapted for Azure's URL structure
// and auth scheme.
//
//	Chat  : {endpoint}/openai/deployments/{deployment}/chat/completions?api-version={version}
//	Embed : {endpoint}/openai/deployments/{deployment}/embeddings?api-version={version}
//	Image : {endpoint}/openai/deployments/{deployment}/images/generations?api-version={version}
package azure

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

	"github.com/samcharles93/ai-sdk/pkg/chat"
	"github.com/samcharles93/ai-sdk/pkg/embed"
	"github.com/samcharles93/ai-sdk/pkg/image"
)

const (
	defaultAPIVersion = "2024-02-01"
	defaultTimeout    = 5 * time.Minute
)

// Config configures an Azure OpenAI Provider.
type Config struct {
	// APIKey is the Azure OpenAI API key. Required.
	APIKey string
	// Endpoint is the Azure OpenAI resource endpoint, e.g.
	// "https://myresource.openai.azure.com". Required.
	Endpoint string
	// Deployment is the model deployment name. Required.
	Deployment string
	// APIVersion overrides the API version query parameter.
	// Defaults to "2024-02-01".
	APIVersion string
	// HTTPClient overrides the HTTP client used for requests.
	HTTPClient *http.Client
}

// Provider implements chat.Provider, embed.Provider, and
// image.Provider for the Azure OpenAI Service.
type Provider struct {
	apiKey     string
	endpoint   string
	deployment string
	apiVersion string
	client     *http.Client
}

// Compile-time interface assertions.
var (
	_ chat.Provider  = (*Provider)(nil)
	_ embed.Provider = (*Provider)(nil)
	_ image.Provider = (*Provider)(nil)
)

// New returns a new Azure OpenAI Provider. It returns an error if APIKey,
// Endpoint, or Deployment is empty.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("azure: APIKey is required: %w", chat.ErrInvalidRequest)
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("azure: Endpoint is required: %w", chat.ErrInvalidRequest)
	}
	if cfg.Deployment == "" {
		return nil, fmt.Errorf("azure: Deployment is required: %w", chat.ErrInvalidRequest)
	}
	ep := strings.TrimRight(cfg.Endpoint, "/")
	av := cfg.APIVersion
	if av == "" {
		av = defaultAPIVersion
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	return &Provider{
		apiKey:     cfg.APIKey,
		endpoint:   ep,
		deployment: cfg.Deployment,
		apiVersion: av,
		client:     hc,
	}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "azure" }

// --- URL builders ----------------------------------------------------------

func (p *Provider) chatURL() string {
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		p.endpoint, p.deployment, p.apiVersion)
}

func (p *Provider) embedURL() string {
	return fmt.Sprintf("%s/openai/deployments/%s/embeddings?api-version=%s",
		p.endpoint, p.deployment, p.apiVersion)
}

func (p *Provider) imageURL() string {
	return fmt.Sprintf("%s/openai/deployments/%s/images/generations?api-version=%s",
		p.endpoint, p.deployment, p.apiVersion)
}

// --- wire types (chat) -----------------------------------------------------

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

// --- chat request building -------------------------------------------------

func (p *Provider) buildChatBody(req chat.Request, stream bool) (map[string]any, []chat.Warning, error) {
	if req.Model == "" {
		return nil, nil, fmt.Errorf("azure: model is required: %w", chat.ErrInvalidRequest)
	}
	if len(req.Messages) == 0 {
		return nil, nil, fmt.Errorf("azure: at least one message is required: %w", chat.ErrInvalidRequest)
	}
	var warnings []chat.Warning
	msgs := make([]wireMessage, len(req.Messages))
	for i, m := range req.Messages {
		wm := wireMessage{Role: string(m.Role), Name: m.Name}
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
					Message: "azure: ImagePart not supported by current models",
				})
			case chat.FilePart:
				warnings = append(warnings, chat.Warning{
					Type:    "unsupported-content",
					Message: "azure: FilePart not supported by current models",
				})
			case chat.ReasoningPart:
				if m.Role == chat.RoleAssistant {
					warnings = append(warnings, chat.Warning{
						Type:    "unsupported-content",
						Message: "azure: ReasoningPart on assistant input dropped; Azure OpenAI has no reasoning replay mechanism",
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
				return nil, nil, fmt.Errorf("azure: tool message at index %d missing ToolCallID: %w", i, chat.ErrInvalidRequest)
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
				return nil, nil, fmt.Errorf("azure: tool_choice type=tool requires Name: %w", chat.ErrInvalidRequest)
			}
			body["tool_choice"] = map[string]any{
				"type":     "function",
				"function": map[string]any{"name": req.ToolChoice.Name},
			}
		default:
			return nil, nil, fmt.Errorf("azure: unknown tool_choice type %q: %w", req.ToolChoice.Type, chat.ErrInvalidRequest)
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
	if stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	return body, warnings, nil
}

func (p *Provider) newChatRequest(ctx context.Context, body map[string]any) (*http.Request, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("azure: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.chatURL(), bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("azure: build request: %w", err)
	}
	httpReq.Header.Set("api-key", p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	return httpReq, nil
}

// classifyChatError maps an HTTP error response to a wrapped sentinel error.
func (p *Provider) classifyChatError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	snippet := strings.TrimSpace(string(b))
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
	return fmt.Errorf("azure: status %d: %s: %w", resp.StatusCode, snippet, base)
}

// --- Chat (non-streaming) --------------------------------------------------

// Chat performs a non-streaming chat completion.
func (p *Provider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	body, warnings, err := p.buildChatBody(req, false)
	if err != nil {
		return chat.Response{}, err
	}
	httpReq, err := p.newChatRequest(ctx, body)
	if err != nil {
		return chat.Response{}, err
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return chat.Response{}, fmt.Errorf("azure: http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return chat.Response{}, p.classifyChatError(resp)
	}
	var wr wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return chat.Response{}, fmt.Errorf("azure: decode response: %w", err)
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
		out.Content = c.Message.Content
		out.FinishReason = c.FinishReason
		if c.Message.Role != "" {
			out.Role = chat.Role(c.Message.Role)
		}
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

// --- ChatStream ------------------------------------------------------------

// ChatStream performs a streaming chat completion. Callers must Close
// the returned Stream when finished.
func (p *Provider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	body, warnings, err := p.buildChatBody(req, true)
	if err != nil {
		return nil, err
	}
	httpReq, err := p.newChatRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("azure: http do: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := p.classifyChatError(resp)
		resp.Body.Close()
		return nil, err
	}
	return &chatStream{
		resp:            resp,
		reader:          bufio.NewReader(resp.Body),
		pendingWarnings: warnings,
	}, nil
}

type chatStream struct {
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
func (s *chatStream) Next(ctx context.Context) (chat.Chunk, error) {
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
			return chat.Chunk{}, fmt.Errorf("azure: stream read: %w", err)
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
			return chat.Chunk{}, fmt.Errorf("azure: decode stream chunk: %w", jerr)
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
			final := chat.Chunk{
				Done:         true,
				Role:         role,
				FinishReason: c.FinishReason,
				Usage:        s.pendingUsage,
			}
			s.bufferedFinal = &final
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
func (s *chatStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.resp != nil && s.resp.Body != nil {
		return s.resp.Body.Close()
	}
	return nil
}

// --- Embed -----------------------------------------------------------------

type wireEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type wireEmbedding struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type wireEmbedUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type wireEmbedResponse struct {
	Object string          `json:"object"`
	Data   []wireEmbedding `json:"data"`
	Model  string          `json:"model"`
	Usage  wireEmbedUsage  `json:"usage"`
}

// classifyEmbedError maps an HTTP error response to a wrapped sentinel error.
func (p *Provider) classifyEmbedError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	snippet := strings.TrimSpace(string(b))
	var base error
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		base = embed.ErrAuthFailed
	case resp.StatusCode == http.StatusTooManyRequests:
		base = embed.ErrRateLimited
	case resp.StatusCode == http.StatusBadRequest:
		base = embed.ErrInvalidRequest
	case resp.StatusCode >= 500:
		base = embed.ErrProviderUnavailable
	default:
		base = embed.ErrProviderUnavailable
	}
	return fmt.Errorf("azure: status %d: %s: %w", resp.StatusCode, snippet, base)
}

// Embed produces embedding vectors for the given inputs.
func (p *Provider) Embed(ctx context.Context, req embed.Request) (embed.Response, error) {
	if req.Model == "" {
		return embed.Response{}, fmt.Errorf("azure: model is required: %w", embed.ErrInvalidRequest)
	}
	if len(req.Inputs) == 0 {
		return embed.Response{}, fmt.Errorf("azure: at least one input is required: %w", embed.ErrInvalidRequest)
	}
	body := wireEmbedRequest{
		Model: req.Model,
		Input: req.Inputs,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return embed.Response{}, fmt.Errorf("azure: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.embedURL(), bytes.NewReader(buf))
	if err != nil {
		return embed.Response{}, fmt.Errorf("azure: build request: %w", err)
	}
	httpReq.Header.Set("api-key", p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return embed.Response{}, fmt.Errorf("azure: http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return embed.Response{}, p.classifyEmbedError(resp)
	}
	var wr wireEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return embed.Response{}, fmt.Errorf("azure: decode response: %w", err)
	}
	out := embed.Response{
		Model:      wr.Model,
		Embeddings: make([]embed.Embedding, len(wr.Data)),
		Usage: embed.Usage{
			PromptTokens: wr.Usage.PromptTokens,
			TotalTokens:  wr.Usage.TotalTokens,
		},
	}
	for _, d := range wr.Data {
		out.Embeddings[d.Index] = embed.Embedding{
			Index:  d.Index,
			Vector: d.Embedding,
		}
	}
	return out, nil
}

// --- Image -----------------------------------------------------------------

type wireImageRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	N      int    `json:"n,omitempty"`
	Size   string `json:"size,omitempty"`
}

type wireImageData struct {
	URL       string `json:"url,omitempty"`
	B64JSON   string `json:"b64_json,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

type wireImageResponse struct {
	Created int64           `json:"created"`
	Data    []wireImageData `json:"data"`
}

// classifyImageError maps an HTTP error response to a wrapped sentinel error.
func (p *Provider) classifyImageError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	snippet := strings.TrimSpace(string(b))
	var base error
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		base = image.ErrAuthFailed
	case resp.StatusCode == http.StatusTooManyRequests:
		base = image.ErrRateLimited
	case resp.StatusCode == http.StatusBadRequest:
		base = image.ErrInvalidRequest
	case resp.StatusCode >= 500:
		base = image.ErrProviderUnavailable
	default:
		base = image.ErrProviderUnavailable
	}
	return fmt.Errorf("azure: status %d: %s: %w", resp.StatusCode, snippet, base)
}

// GenerateImage creates one or more images from the given prompt.
func (p *Provider) GenerateImage(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
	if req.Model == "" {
		return image.GenerateImageResponse{}, fmt.Errorf("azure: model is required: %w", image.ErrInvalidRequest)
	}
	if req.Prompt == "" {
		return image.GenerateImageResponse{}, fmt.Errorf("azure: prompt is required: %w", image.ErrInvalidRequest)
	}
	wireReq := wireImageRequest{
		Model:  req.Model,
		Prompt: req.Prompt,
		N:      req.N,
		Size:   req.Size,
	}
	if wireReq.N == 0 {
		wireReq.N = 1
	}
	if wireReq.Size == "" {
		wireReq.Size = "1024x1024"
	}
	buf, err := json.Marshal(wireReq)
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("azure: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.imageURL(), bytes.NewReader(buf))
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("azure: build request: %w", err)
	}
	httpReq.Header.Set("api-key", p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("azure: http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return image.GenerateImageResponse{}, p.classifyImageError(resp)
	}
	var wr wireImageResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("azure: decode response: %w", err)
	}
	out := image.GenerateImageResponse{
		Images: make([]image.GeneratedImage, len(wr.Data)),
	}
	for i, d := range wr.Data {
		out.Images[i] = image.GeneratedImage{
			URL:       d.URL,
			Base64:    d.B64JSON,
			MediaType: d.MediaType,
		}
	}
	return out, nil
}
