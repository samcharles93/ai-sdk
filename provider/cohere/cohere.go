// Package cohere implements chat.Provider, embed.Provider and rerank.Provider
// for the Cohere API (https://api.cohere.com/v1). Cohere uses its own API
// format and is not OpenAI-compatible.
package cohere

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
	"github.com/samcharles93/ai-sdk/embed"
	"github.com/samcharles93/ai-sdk/rerank"
)

const (
	defaultBaseURL = "https://api.cohere.com/v1"
	defaultTimeout = 5 * time.Minute
)

// Config configures a Cohere Provider.
type Config struct {
	// APIKey is the Cohere API key. Required.
	APIKey string
	// BaseURL overrides the API base URL. Defaults to https://api.cohere.com/v1.
	BaseURL string
	// HTTPClient overrides the HTTP client used for requests.
	HTTPClient *http.Client
}

// Provider implements chat.Provider, embed.Provider and rerank.Provider
// backed by the Cohere API.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// Compile-time assertions.
var (
	_ chat.Provider   = (*Provider)(nil)
	_ embed.Provider  = (*Provider)(nil)
	_ rerank.Provider = (*Provider)(nil)
)

// New returns a new Cohere Provider. It returns an error if APIKey is empty.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("cohere: APIKey is required: %w", chat.ErrInvalidRequest)
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
func (p *Provider) Name() string { return "cohere" }

// ---------------------------------------------------------------------------
// wire types — chat
// ---------------------------------------------------------------------------

// cohereRole maps domain roles to Cohere wire roles.
func cohereRole(r chat.Role) string {
	switch r {
	case chat.RoleSystem:
		return "SYSTEM"
	case chat.RoleUser:
		return "USER"
	case chat.RoleAssistant:
		return "CHATBOT"
	case chat.RoleTool:
		return "TOOL"
	default:
		return "USER"
	}
}

type cohereChatMessage struct {
	Role    string `json:"role"`
	Message string `json:"message,omitempty"`
}

type cohereToolDef struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	ParameterDefinitions json.RawMessage `json:"parameter_definitions,omitempty"`
}

type cohereChatRequest struct {
	Message     string              `json:"message"`
	ChatHistory []cohereChatMessage `json:"chat_history,omitempty"`
	Model       string              `json:"model"`
	Stream      bool                `json:"stream"`
	Tools       []cohereToolDef     `json:"tools,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature float32             `json:"temperature,omitempty"`
	P           float32             `json:"p,omitempty"`
	K           int                 `json:"k,omitempty"`
}

type cohereToolCall struct {
	Name       string          `json:"name"`
	Parameters json.RawMessage `json:"parameters"`
}

type cohereChatResponse struct {
	Text         string            `json:"text"`
	GenerationID string            `json:"generation_id"`
	ChatHistory  []json.RawMessage `json:"chat_history,omitempty"`
	FinishReason string            `json:"finish_reason,omitempty"`
	ToolCalls    []cohereToolCall  `json:"tool_calls,omitempty"`
	Meta         *cohereMeta       `json:"meta,omitempty"`
	IsFinished   bool              `json:"is_finished,omitempty"`
}

type cohereMeta struct {
	Tokens      *cohereTokens      `json:"tokens,omitempty"`
	BilledUnits *cohereBilledUnits `json:"billed_units,omitempty"`
}

type cohereTokens struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type cohereBilledUnits struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func finishReason(r string) string {
	switch r {
	case "COMPLETE":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "ERROR", "ERROR_TOXIC", "ERROR_LIMIT":
		return ""
	default:
		return strings.ToLower(r)
	}
}

// --- stream types ----------------------------------------------------------

type cohereStreamEvent struct {
	IsFinished   bool                `json:"is_finished"`
	EventType    string              `json:"event_type"`
	Text         string              `json:"text,omitempty"`
	ToolCalls    []cohereToolCall    `json:"tool_calls,omitempty"`
	FinishReason string              `json:"finish_reason,omitempty"`
	Response     *cohereChatResponse `json:"response,omitempty"`
}

// ---------------------------------------------------------------------------
// chat — request building
// ---------------------------------------------------------------------------

func (p *Provider) buildChatBody(req chat.Request) (cohereChatRequest, []chat.Warning, error) {
	if req.Model == "" {
		return cohereChatRequest{}, nil, fmt.Errorf("cohere: model is required: %w", chat.ErrInvalidRequest)
	}
	if len(req.Messages) == 0 {
		return cohereChatRequest{}, nil, fmt.Errorf("cohere: at least one message is required: %w", chat.ErrInvalidRequest)
	}

	var warnings []chat.Warning
	// Find the last USER message — it becomes the "message" field.
	// All other messages go into chat_history.
	lastUserIdx := -1
	for i, m := range req.Messages {
		if m.Role == chat.RoleUser {
			lastUserIdx = i
		}
	}
	if lastUserIdx < 0 {
		return cohereChatRequest{}, nil, fmt.Errorf("cohere: at least one user message is required: %w", chat.ErrInvalidRequest)
	}

	currentMsg := req.Messages[lastUserIdx].Text()
	body := cohereChatRequest{
		Message:     currentMsg,
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		P:           req.TopP,
	}

	// Build chat_history from all messages except the last user message.
	for i, m := range req.Messages {
		if i == lastUserIdx {
			continue
		}
		cm := cohereChatMessage{Role: cohereRole(m.Role)}
		// For non-TOOL messages, extract text content.
		if m.Role != chat.RoleTool {
			cm.Message = m.Text()
		} else {
			// TOOL messages carry their output as text.
			cm.Message = m.Text()
		}
		body.ChatHistory = append(body.ChatHistory, cm)
	}

	// Build tools.
	if len(req.Tools) > 0 {
		body.Tools = make([]cohereToolDef, len(req.Tools))
		for i, t := range req.Tools {
			body.Tools[i] = cohereToolDef{
				Name:                 t.Name,
				Description:          t.Description,
				ParameterDefinitions: t.Parameters,
			}
		}
	}

	return body, warnings, nil
}

// ---------------------------------------------------------------------------
// http helpers
// ---------------------------------------------------------------------------

func (p *Provider) doChat(ctx context.Context, body cohereChatRequest) (*http.Response, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("cohere: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("cohere: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	return p.client.Do(httpReq)
}

func classifyHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	snippet := strings.TrimSpace(string(body))
	var base error
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
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
	return fmt.Errorf("cohere: status %d: %s: %w", resp.StatusCode, snippet, base)
}

// ---------------------------------------------------------------------------
// Chat (non-streaming)
// ---------------------------------------------------------------------------

// Chat performs a non-streaming chat completion.
func (p *Provider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	body, warnings, err := p.buildChatBody(req)
	if err != nil {
		return chat.Response{}, err
	}
	body.Stream = false

	resp, err := p.doChat(ctx, body)
	if err != nil {
		return chat.Response{}, fmt.Errorf("cohere: http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return chat.Response{}, classifyHTTPError(resp)
	}

	var cr cohereChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return chat.Response{}, fmt.Errorf("cohere: decode response: %w", err)
	}

	out := chat.Response{
		Model:    req.Model,
		Role:     chat.RoleAssistant,
		Content:  cr.Text,
		Warnings: warnings,
		ID:       cr.GenerationID,
	}

	// Map finish reason.
	out.FinishReason = finishReason(cr.FinishReason)

	// Populate Parts.
	if cr.Text != "" {
		out.Parts = chat.Parts{chat.TextPart{Text: cr.Text}}
	}

	// Tool calls — Cohere returns parameters as a JSON object, convert to
	// JSON string for the canonical ToolCall.Arguments.
	if len(cr.ToolCalls) > 0 {
		tcs := make([]chat.ToolCall, len(cr.ToolCalls))
		for i, tc := range cr.ToolCalls {
			argsStr := string(tc.Parameters)
			// If parameters is a valid JSON object, use it as-is.
			if len(tc.Parameters) > 0 && tc.Parameters[0] == '{' {
				argsStr = string(tc.Parameters)
			}
			tcs[i] = chat.ToolCall{
				ID:        fmt.Sprintf("call_%d", i),
				Name:      tc.Name,
				Arguments: argsStr,
			}
		}
		out.ToolCalls = tcs
		if out.FinishReason == "" || out.FinishReason == "stop" {
			out.FinishReason = "tool_calls"
		}
	}

	// Usage from meta.
	if cr.Meta != nil && cr.Meta.Tokens != nil {
		out.Usage = chat.Usage{
			PromptTokens:     cr.Meta.Tokens.InputTokens,
			CompletionTokens: cr.Meta.Tokens.OutputTokens,
			TotalTokens:      cr.Meta.Tokens.InputTokens + cr.Meta.Tokens.OutputTokens,
		}
	} else if cr.Meta != nil && cr.Meta.BilledUnits != nil {
		out.Usage = chat.Usage{
			PromptTokens:     cr.Meta.BilledUnits.InputTokens,
			CompletionTokens: cr.Meta.BilledUnits.OutputTokens,
			TotalTokens:      cr.Meta.BilledUnits.InputTokens + cr.Meta.BilledUnits.OutputTokens,
		}
	}

	return out, nil
}

// ---------------------------------------------------------------------------
// ChatStream
// ---------------------------------------------------------------------------

// stream implements chat.Stream for Cohere SSE streaming.
type stream struct {
	resp            *http.Response
	reader          *bufio.Reader
	closed          bool
	finished        bool
	pendingWarnings []chat.Warning
	doneEmitted     bool
	pendingUsage    *chat.Usage
}

// ChatStream performs a streaming chat completion. Callers must Close
// the returned Stream when finished.
func (p *Provider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	body, warnings, err := p.buildChatBody(req)
	if err != nil {
		return nil, err
	}
	body.Stream = true

	resp, err := p.doChat(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("cohere: http do: %w", err)
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
				if !s.doneEmitted {
					s.doneEmitted = true
					return chat.Chunk{Done: true, Usage: s.pendingUsage}, nil
				}
				return chat.Chunk{}, io.EOF
			}
			return chat.Chunk{}, fmt.Errorf("cohere: stream read: %w", err)
		}
		trimmed := bytes.TrimRight(line, "\r\n")
		if len(trimmed) == 0 {
			continue
		}
		// Cohere streams may use event: lines and data: lines (SSE).
		if bytes.HasPrefix(trimmed, []byte("event:")) {
			continue
		}
		if bytes.HasPrefix(trimmed, []byte(":")) {
			continue
		}
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			// Some Cohere SDKs send raw JSON lines.
			if trimmed[0] == '{' {
				return s.processStreamLine(trimmed)
			}
			continue
		}
		data := bytes.TrimSpace(trimmed[len("data:"):])
		if len(data) == 0 {
			continue
		}
		if data[0] != '{' {
			continue
		}
		return s.processStreamLine(data)
	}
}

func (s *stream) processStreamLine(data []byte) (chat.Chunk, error) {
	var ev cohereStreamEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return chat.Chunk{}, fmt.Errorf("cohere: decode stream event: %w", err)
	}

	switch ev.EventType {
	case "text-generation":
		return chat.Chunk{
			Delta: ev.Text,
			Role:  chat.RoleAssistant,
		}, nil
	case "tool-calls-generation":
		// Tool call delta — Cohere sends full tool call objects in streaming.
		deltas := make([]chat.ToolCallDelta, len(ev.ToolCalls))
		for i, tc := range ev.ToolCalls {
			argsStr := string(tc.Parameters)
			deltas[i] = chat.ToolCallDelta{
				Index:     i,
				Name:      tc.Name,
				ArgsDelta: argsStr,
			}
		}
		return chat.Chunk{
			Role:           chat.RoleAssistant,
			ToolCallDeltas: deltas,
		}, nil
	case "stream-end":
		s.finished = true
		fr := finishReason(ev.FinishReason)
		out := chat.Chunk{
			Done:         true,
			Role:         chat.RoleAssistant,
			FinishReason: fr,
			Usage:        s.pendingUsage,
		}
		if ev.Response != nil {
			if ev.Response.Meta != nil && ev.Response.Meta.Tokens != nil {
				u := &chat.Usage{
					PromptTokens:     ev.Response.Meta.Tokens.InputTokens,
					CompletionTokens: ev.Response.Meta.Tokens.OutputTokens,
					TotalTokens:      ev.Response.Meta.Tokens.InputTokens + ev.Response.Meta.Tokens.OutputTokens,
				}
				out.Usage = u
			} else if ev.Response.Meta != nil && ev.Response.Meta.BilledUnits != nil {
				u := &chat.Usage{
					PromptTokens:     ev.Response.Meta.BilledUnits.InputTokens,
					CompletionTokens: ev.Response.Meta.BilledUnits.OutputTokens,
					TotalTokens:      ev.Response.Meta.BilledUnits.InputTokens + ev.Response.Meta.BilledUnits.OutputTokens,
				}
				out.Usage = u
			}
		}
		return out, nil
	default:
		return chat.Chunk{}, nil
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

// ---------------------------------------------------------------------------
// Embed
// ---------------------------------------------------------------------------

type cohereEmbedRequest struct {
	Texts     []string `json:"texts"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type"`
}

type cohereEmbedResponse struct {
	ID         string           `json:"id"`
	Texts      []string         `json:"texts"`
	Embeddings [][]float32      `json:"embeddings"`
	Meta       *cohereEmbedMeta `json:"meta,omitempty"`
}

type cohereEmbedMeta struct {
	BilledUnits *cohereBilledUnits `json:"billed_units,omitempty"`
}

// Embed produces one embedding vector per entry in req.Inputs.
func (p *Provider) Embed(ctx context.Context, req embed.Request) (embed.Response, error) {
	if req.Model == "" {
		return embed.Response{}, fmt.Errorf("cohere: embed model is required: %w", chat.ErrInvalidRequest)
	}
	if len(req.Inputs) == 0 {
		return embed.Response{}, fmt.Errorf("cohere: at least one input is required: %w", chat.ErrInvalidRequest)
	}

	body := cohereEmbedRequest{
		Texts:     req.Inputs,
		Model:     req.Model,
		InputType: "search_document",
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return embed.Response{}, fmt.Errorf("cohere: marshal embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/embed", bytes.NewReader(buf))
	if err != nil {
		return embed.Response{}, fmt.Errorf("cohere: build embed request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return embed.Response{}, fmt.Errorf("cohere: embed http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return embed.Response{}, classifyHTTPError(resp)
	}

	var er cohereEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return embed.Response{}, fmt.Errorf("cohere: decode embed response: %w", err)
	}

	out := embed.Response{
		Model:      req.Model,
		Embeddings: make([]embed.Embedding, len(er.Embeddings)),
	}
	for i, vec := range er.Embeddings {
		out.Embeddings[i] = embed.Embedding{
			Index:  i,
			Vector: vec,
		}
	}
	if er.Meta != nil && er.Meta.BilledUnits != nil {
		out.Usage = embed.Usage{
			PromptTokens: er.Meta.BilledUnits.InputTokens,
			TotalTokens:  er.Meta.BilledUnits.InputTokens,
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Rerank
// ---------------------------------------------------------------------------

type cohereRerankRequest struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	Model     string   `json:"model"`
	TopN      int      `json:"top_n,omitempty"`
}

type cohereRerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

type cohereRerankResponse struct {
	Results []cohereRerankResult `json:"results"`
	Meta    *cohereRerankMeta    `json:"meta,omitempty"`
}

type cohereRerankMeta struct {
	BilledUnits *cohereBilledUnits `json:"billed_units,omitempty"`
}

// Rerank re-orders documents by relevance to the query.
func (p *Provider) Rerank(ctx context.Context, req rerank.Request) (rerank.Response, error) {
	if req.Model == "" {
		return rerank.Response{}, fmt.Errorf("cohere: rerank model is required: %w", chat.ErrInvalidRequest)
	}
	if req.Query == "" {
		return rerank.Response{}, fmt.Errorf("cohere: query is required: %w", chat.ErrInvalidRequest)
	}
	if len(req.Documents) == 0 {
		return rerank.Response{}, fmt.Errorf("cohere: at least one document is required: %w", chat.ErrInvalidRequest)
	}

	body := cohereRerankRequest{
		Query:     req.Query,
		Documents: req.Documents,
		Model:     req.Model,
		TopN:      req.TopN,
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return rerank.Response{}, fmt.Errorf("cohere: marshal rerank request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/rerank", bytes.NewReader(buf))
	if err != nil {
		return rerank.Response{}, fmt.Errorf("cohere: build rerank request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return rerank.Response{}, fmt.Errorf("cohere: rerank http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return rerank.Response{}, classifyHTTPError(resp)
	}

	var rr cohereRerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return rerank.Response{}, fmt.Errorf("cohere: decode rerank response: %w", err)
	}

	out := rerank.Response{
		Model:   req.Model,
		Ranking: make([]rerank.RankingItem, len(rr.Results)),
	}
	for i, r := range rr.Results {
		doc := ""
		if r.Index < len(req.Documents) {
			doc = req.Documents[r.Index]
		}
		out.Ranking[i] = rerank.RankingItem{
			OriginalIndex: r.Index,
			Score:         r.RelevanceScore,
			Document:      doc,
		}
	}
	return out, nil
}
