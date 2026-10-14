package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
)

const (
	// interactionsAPIRevision selects the steps schema; the outputs schema
	// was removed on 2026-06-08.
	interactionsAPIRevision = "2026-05-20"
	// maxForegroundOutputTokens is the largest max_output_tokens the API
	// accepts without background execution.
	maxForegroundOutputTokens = 65536
	defaultPollInterval       = 2 * time.Second
	maxStreamReconnects       = 3
)

// Interactions implements chat.Provider over the Gemini Interactions API.
//
// Requests are stateless: the full history is sent as steps with store
// disabled, and model steps are replayed verbatim through message provider
// options so thought signatures survive. Requests above 65,536 output tokens
// (long decode) must run in the background with storage enabled; those
// interactions are deleted once their result has been read.
type Interactions struct {
	apiKey       string
	baseURL      string
	client       *http.Client
	pollInterval time.Duration
}

// NewInteractions constructs an Interactions provider. APIKey is required.
func NewInteractions(cfg Config) (*Interactions, error) {
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return &Interactions{apiKey: p.apiKey, baseURL: p.baseURL, client: p.client, pollInterval: defaultPollInterval}, nil
}

// Name returns the provider identifier.
func (p *Interactions) Name() string { return "gemini" }

var _ chat.Provider = (*Interactions)(nil)

// interactionsOptions is the request provider-options bucket "gemini".
type interactionsOptions struct {
	ThinkingLevel     string `json:"thinking_level,omitempty"`
	ThinkingSummaries string `json:"thinking_summaries,omitempty"`
}

// interactionsMessageOptions is the message provider-options bucket "gemini"
// carrying the model steps of an assistant turn for verbatim replay.
type interactionsMessageOptions struct {
	InteractionSteps []json.RawMessage `json:"interaction_steps,omitempty"`
}

// --- wire types -------------------------------------------------------------

type ixContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	URI      string `json:"uri,omitempty"`
}

type ixStep struct {
	Type      string          `json:"type"`
	Content   []ixContent     `json:"content,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Result    []ixContent     `json:"result,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Summary   []ixContent     `json:"summary,omitempty"`
}

type ixUsage struct {
	TotalInputTokens   int `json:"total_input_tokens"`
	TotalOutputTokens  int `json:"total_output_tokens"`
	TotalThoughtTokens int `json:"total_thought_tokens"`
	TotalCachedTokens  int `json:"total_cached_tokens"`
	TotalTokens        int `json:"total_tokens"`
}

func (u *ixUsage) toUsage() chat.Usage {
	return chat.Usage{
		PromptTokens:     u.TotalInputTokens,
		CompletionTokens: u.TotalOutputTokens + u.TotalThoughtTokens,
		TotalTokens:      u.TotalTokens,
		CachedTokens:     u.TotalCachedTokens,
	}
}

type ixError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ixInteraction struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Steps  []json.RawMessage `json:"steps"`
	Usage  *ixUsage          `json:"usage"`
	Errors []ixError         `json:"errors"`
}

type ixEvent struct {
	EventType     string          `json:"event_type"`
	EventID       string          `json:"event_id"`
	Index         int             `json:"index"`
	Step          *ixStep         `json:"step"`
	Delta         json.RawMessage `json:"delta"`
	Interaction   *ixInteraction  `json:"interaction"`
	InteractionID string          `json:"interaction_id"`
	Status        string          `json:"status"`
	Error         *ixError        `json:"error"`
}

type ixDelta struct {
	Type      string     `json:"type"`
	Text      string     `json:"text"`
	Signature string     `json:"signature"`
	Arguments string     `json:"arguments"`
	Content   *ixContent `json:"content"`
}

// --- request ----------------------------------------------------------------

func ixContentFromParts(parts chat.Parts) []ixContent {
	var out []ixContent
	for _, part := range parts {
		switch p := part.(type) {
		case chat.TextPart:
			if p.Text != "" {
				out = append(out, ixContent{Type: "text", Text: p.Text})
			}
		case chat.ImagePart:
			mt := p.MediaType
			if mt == "" && len(p.Data) > 0 {
				mt = "image/png"
			}
			out = append(out, ixMedia("image", mt, p.Data, p.URL))
		case chat.FilePart:
			out = append(out, ixMedia(mediaKind(p.MediaType), p.MediaType, p.Data, p.URL))
		}
	}
	return out
}

func ixMedia(kind, mimeType string, data []byte, uri string) ixContent {
	if len(data) > 0 {
		return ixContent{Type: kind, MimeType: mimeType, Data: base64.StdEncoding.EncodeToString(data)}
	}
	return ixContent{Type: kind, MimeType: mimeType, URI: uri}
}

func mediaKind(mimeType string) string {
	for _, kind := range []string{"image", "audio", "video"} {
		if strings.HasPrefix(mimeType, kind+"/") {
			return kind
		}
	}
	return "document"
}

func assistantSteps(m chat.Message) ([]json.RawMessage, error) {
	opts, err := chat.ProviderOptionsFor[interactionsMessageOptions](m.ProviderOptions, "gemini")
	if err != nil {
		return nil, err
	}
	if len(opts.InteractionSteps) > 0 {
		return opts.InteractionSteps, nil
	}
	var steps []any
	if content := ixContentFromParts(m.GetParts()); len(content) > 0 {
		steps = append(steps, ixStep{Type: "model_output", Content: content})
	}
	for _, tc := range m.ToolCalls {
		args := json.RawMessage(`{}`)
		if strings.TrimSpace(tc.Arguments) != "" {
			if !json.Valid([]byte(tc.Arguments)) {
				return nil, fmt.Errorf("assistant tool call %q has invalid JSON arguments: %w", tc.Name, chat.ErrInvalidRequest)
			}
			args = json.RawMessage(tc.Arguments)
		}
		steps = append(steps, ixStep{Type: "function_call", ID: tc.ID, Name: tc.Name, Arguments: args})
	}
	raw := make([]json.RawMessage, 0, len(steps))
	for _, s := range steps {
		b, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		raw = append(raw, b)
	}
	return raw, nil
}

func (p *Interactions) buildBody(req chat.Request, stream bool) (map[string]any, error) {
	var system []string
	var input []any
	toolNames := map[string]string{}
	for _, m := range req.Messages {
		switch m.Role {
		case chat.RoleSystem:
			if t := m.Text(); t != "" {
				system = append(system, t)
			}
		case chat.RoleAssistant:
			for _, tc := range m.ToolCalls {
				toolNames[tc.ID] = tc.Name
			}
			steps, err := assistantSteps(m)
			if err != nil {
				return nil, err
			}
			for _, s := range steps {
				input = append(input, s)
			}
		case chat.RoleTool:
			name := m.Name
			if name == "" {
				name = toolNames[m.ToolCallID]
			}
			input = append(input, ixStep{
				Type: "function_result", Name: name, CallID: m.ToolCallID,
				Result: []ixContent{{Type: "text", Text: m.Text()}},
			})
		default:
			content := ixContentFromParts(m.GetParts())
			if len(content) == 0 {
				content = []ixContent{{Type: "text", Text: ""}}
			}
			input = append(input, ixStep{Type: "user_input", Content: content})
		}
	}

	body := map[string]any{"model": req.Model, "input": input, "store": false}
	if stream {
		body["stream"] = true
	}
	if req.MaxTokens > maxForegroundOutputTokens {
		body["background"] = true
		body["store"] = true
	}
	if len(system) > 0 {
		body["system_instruction"] = strings.Join(system, "\n")
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tool := map[string]any{"type": "function", "name": t.Name}
			if t.Description != "" {
				tool["description"] = t.Description
			}
			if len(t.Parameters) > 0 {
				tool["parameters"] = t.Parameters
			}
			tools = append(tools, tool)
		}
		body["tools"] = tools
	}

	gen := map[string]any{}
	if req.MaxTokens != 0 {
		gen["max_output_tokens"] = req.MaxTokens
	}
	if req.Temperature != 0 && req.ModelInfo.TemperatureSupported() {
		gen["temperature"] = req.Temperature
	}
	if req.TopP != 0 {
		gen["top_p"] = req.TopP
	}
	if len(req.Stop) > 0 {
		gen["stop_sequences"] = req.Stop
	}
	opts, err := chat.ProviderOptionsFor[interactionsOptions](req.ProviderOptions, "gemini")
	if err != nil {
		return nil, err
	}
	if opts.ThinkingLevel != "" {
		gen["thinking_level"] = opts.ThinkingLevel
	}
	if opts.ThinkingSummaries != "" {
		gen["thinking_summaries"] = opts.ThinkingSummaries
	}
	if req.ToolChoice != nil {
		switch req.ToolChoice.Type {
		case chat.ToolChoiceAuto:
			gen["tool_choice"] = "auto"
		case chat.ToolChoiceNone:
			gen["tool_choice"] = "none"
		case chat.ToolChoiceRequired:
			gen["tool_choice"] = "any"
		case chat.ToolChoiceTool:
			if req.ToolChoice.Name == "" {
				return nil, fmt.Errorf("ToolChoiceTool requires Name: %w", chat.ErrInvalidRequest)
			}
			gen["tool_choice"] = map[string]any{
				"allowed_tools": map[string]any{"mode": "any", "tools": []string{req.ToolChoice.Name}},
			}
		default:
			return nil, fmt.Errorf("unknown ToolChoice type %q: %w", req.ToolChoice.Type, chat.ErrInvalidRequest)
		}
	}
	if len(gen) > 0 {
		body["generation_config"] = gen
	}
	return body, nil
}

func validate(req chat.Request) error {
	if strings.TrimSpace(req.Model) == "" {
		return fmt.Errorf("gemini: model is required: %w", chat.ErrInvalidRequest)
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("gemini: at least one message is required: %w", chat.ErrInvalidRequest)
	}
	return nil
}

// --- transport --------------------------------------------------------------

func (p *Interactions) do(ctx context.Context, method, path string, body any, accept string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("gemini: marshal request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, p.baseURL+"/v1beta/interactions"+path, reader)
	if err != nil {
		return nil, fmt.Errorf("gemini: build request: %w", err)
	}
	httpReq.Header.Set("x-goog-api-key", p.apiKey)
	httpReq.Header.Set("Api-Revision", interactionsAPIRevision)
	httpReq.Header.Set("Accept", accept)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: do request: %w", errors.Join(err, chat.ErrProviderUnavailable))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, classifyHTTP(resp)
	}
	return resp, nil
}

func (p *Interactions) fetch(ctx context.Context, method, path string, body any) (ixInteraction, error) {
	resp, err := p.do(ctx, method, path, body, "application/json")
	if err != nil {
		return ixInteraction{}, err
	}
	defer resp.Body.Close()
	var ix ixInteraction
	if err := json.NewDecoder(resp.Body).Decode(&ix); err != nil {
		return ixInteraction{}, fmt.Errorf("gemini: decode interaction: %w", err)
	}
	return ix, nil
}

// discard cancels (when unfinished) and deletes a stored background
// interaction so long-decode prompts do not stay on the server for 7 days.
func (p *Interactions) discard(ctx context.Context, id string, cancel bool) {
	ctx, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer stop()
	escaped := "/" + url.PathEscape(id)
	if cancel {
		if resp, err := p.do(ctx, http.MethodPost, escaped+"/cancel", nil, "application/json"); err == nil {
			resp.Body.Close()
		}
	}
	if resp, err := p.do(ctx, http.MethodDelete, escaped, nil, "application/json"); err == nil {
		resp.Body.Close()
	}
}

func pending(status string) bool { return status == "in_progress" || status == "queued" }

// --- non-streaming ----------------------------------------------------------

// Chat performs a non-streaming chat completion. Long-decode requests are
// polled until the background interaction finishes.
func (p *Interactions) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	if err := validate(req); err != nil {
		return chat.Response{}, err
	}
	body, err := p.buildBody(req, false)
	if err != nil {
		return chat.Response{}, fmt.Errorf("gemini: %w", err)
	}
	ix, err := p.fetch(ctx, http.MethodPost, "", body)
	if err != nil {
		return chat.Response{}, err
	}
	if body["background"] == true {
		defer func() { p.discard(ctx, ix.ID, pending(ix.Status)) }()
		for pending(ix.Status) {
			select {
			case <-ctx.Done():
				return chat.Response{}, ctx.Err()
			case <-time.After(p.pollInterval):
			}
			next, err := p.fetch(ctx, http.MethodGet, "/"+url.PathEscape(ix.ID), nil)
			if err != nil {
				return chat.Response{}, err
			}
			ix = next
		}
	}
	return interactionResponse(req.Model, ix)
}

func interactionResponse(model string, ix ixInteraction) (chat.Response, error) {
	if err := statusError(ix.Status, ix.Errors); err != nil {
		return chat.Response{}, err
	}
	out := chat.Response{ID: ix.ID, Model: model, Role: chat.RoleAssistant}
	var replay []json.RawMessage
	var text strings.Builder
	for i, raw := range ix.Steps {
		var step ixStep
		if err := json.Unmarshal(raw, &step); err != nil {
			return chat.Response{}, fmt.Errorf("gemini: decode step %d: %w", i, err)
		}
		if step.Type == "user_input" || step.Type == "function_result" {
			continue
		}
		replay = append(replay, raw)
		switch step.Type {
		case "thought":
			if summary := contentText(step.Summary); summary != "" {
				out.Parts = append(out.Parts, chat.ReasoningPart{Text: summary})
			}
		case "model_output":
			for _, c := range step.Content {
				switch c.Type {
				case "text":
					text.WriteString(c.Text)
					out.Parts = append(out.Parts, chat.TextPart{Text: c.Text})
				case "image":
					if data, err := base64.StdEncoding.DecodeString(c.Data); err == nil && len(data) > 0 {
						out.Parts = append(out.Parts, chat.ImagePart{Data: data, MediaType: c.MimeType})
					}
				}
			}
		case "function_call":
			args := string(step.Arguments)
			if args == "" {
				args = "{}"
			}
			out.ToolCalls = append(out.ToolCalls, chat.ToolCall{ID: step.ID, Name: step.Name, Arguments: args})
		}
	}
	out.Content = text.String()
	out.FinishReason = finishReason(ix.Status, len(out.ToolCalls) > 0)
	if ix.Usage != nil {
		out.Usage = ix.Usage.toUsage()
	}
	if len(replay) > 0 {
		out.ProviderMetadata = map[string]any{"gemini": interactionsMessageOptions{InteractionSteps: replay}}
	}
	return out, nil
}

func contentText(content []ixContent) string {
	var sb strings.Builder
	for _, c := range content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}

func statusError(status string, errs []ixError) error {
	switch status {
	case "failed":
		msg := "interaction failed"
		if len(errs) > 0 {
			msg = errs[0].Message
		}
		return fmt.Errorf("gemini: %s: %w", msg, chat.ErrProviderUnavailable)
	case "cancelled":
		return fmt.Errorf("gemini: interaction cancelled: %w", chat.ErrProviderUnavailable)
	}
	return nil
}

func finishReason(status string, toolCalls bool) string {
	switch {
	case toolCalls || status == "requires_action":
		return "tool_calls"
	case status == "incomplete":
		return "length"
	default:
		return "stop"
	}
}

// --- streaming --------------------------------------------------------------

// ChatStream performs a streaming chat completion. Long-decode streams
// reconnect from the last event ID when the connection drops.
func (p *Interactions) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	body, err := p.buildBody(req, true)
	if err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}
	resp, err := p.do(ctx, http.MethodPost, "", body, "text/event-stream")
	if err != nil {
		return nil, err
	}
	return &ixStream{
		p: p, resp: resp, reader: bufio.NewReader(resp.Body),
		background: body["background"] == true, toolIndex: map[int]int{}, steps: map[int]*ixStepAcc{},
	}, nil
}

type ixStepAcc struct {
	step ixStep
	text strings.Builder
	args strings.Builder
	sum  strings.Builder
}

func (a *ixStepAcc) raw() json.RawMessage {
	s := a.step
	switch s.Type {
	case "model_output":
		if a.text.Len() > 0 {
			s.Content = append(s.Content, ixContent{Type: "text", Text: a.text.String()})
		}
	case "function_call":
		if a.args.Len() > 0 && json.Valid([]byte(a.args.String())) {
			s.Arguments = json.RawMessage(a.args.String())
		}
		if len(s.Arguments) == 0 {
			s.Arguments = json.RawMessage(`{}`)
		}
	case "thought":
		if a.sum.Len() > 0 {
			s.Summary = []ixContent{{Type: "text", Text: a.sum.String()}}
		}
	}
	b, _ := json.Marshal(s)
	return b
}

type ixStream struct {
	p           *Interactions
	resp        *http.Response
	reader      *bufio.Reader
	background  bool
	id          string
	lastEventID string
	reconnects  int
	toolIndex   map[int]int
	steps       map[int]*ixStepAcc
	order       []int
	status      string
	finished    bool
	closed      bool
}

func (s *ixStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.resp.Body.Close()
	if s.background && s.id != "" {
		s.p.discard(context.Background(), s.id, !s.finished)
	}
	return err
}

// Next returns the next streaming chunk.
func (s *ixStream) Next(ctx context.Context) (chat.Chunk, error) {
	for {
		if err := ctx.Err(); err != nil {
			return chat.Chunk{}, err
		}
		if s.finished {
			return chat.Chunk{}, io.EOF
		}
		data, err := readSSEData(s.reader)
		if err != nil {
			if rerr := s.reconnect(ctx, err); rerr != nil {
				return chat.Chunk{}, rerr
			}
			continue
		}
		if string(data) == "[DONE]" {
			if err := s.reconnect(ctx, io.EOF); err != nil {
				return chat.Chunk{}, err
			}
			continue
		}
		var ev ixEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			continue
		}
		if ev.EventID != "" {
			s.lastEventID = ev.EventID
		}
		chunk, emit, err := s.apply(ev)
		if err != nil {
			return chat.Chunk{}, err
		}
		if emit {
			return chunk, nil
		}
	}
}

// reconnect resumes a background stream that ended before completion. A
// foreground stream cannot be resumed, so it ends with the read error.
func (s *ixStream) reconnect(ctx context.Context, cause error) error {
	if !s.background || s.id == "" || s.reconnects >= maxStreamReconnects {
		if cause == io.EOF {
			return fmt.Errorf("gemini: stream ended before interaction completed: %w", chat.ErrProviderUnavailable)
		}
		return fmt.Errorf("gemini: read stream: %w", cause)
	}
	s.reconnects++
	s.resp.Body.Close()
	q := url.Values{"stream": {"true"}}
	if s.lastEventID != "" {
		q.Set("last_event_id", s.lastEventID)
	}
	resp, err := s.p.do(ctx, http.MethodGet, "/"+url.PathEscape(s.id)+"?"+q.Encode(), nil, "text/event-stream")
	if err != nil {
		return err
	}
	s.resp, s.reader = resp, bufio.NewReader(resp.Body)
	return nil
}

func (s *ixStream) apply(ev ixEvent) (chat.Chunk, bool, error) {
	switch ev.EventType {
	case "interaction.created":
		if ev.Interaction != nil {
			s.id, s.status = ev.Interaction.ID, ev.Interaction.Status
		}
	case "interaction.status_update":
		s.status = ev.Status
	case "step.start":
		if ev.Step == nil {
			return chat.Chunk{}, false, nil
		}
		acc := &ixStepAcc{step: *ev.Step}
		if _, seen := s.steps[ev.Index]; !seen {
			s.order = append(s.order, ev.Index)
		}
		s.steps[ev.Index] = acc
		if acc.step.Type == "function_call" {
			idx := len(s.toolIndex)
			s.toolIndex[ev.Index] = idx
			delta := chat.ToolCallDelta{Index: idx, ID: acc.step.ID, Name: acc.step.Name}
			if len(acc.step.Arguments) > 0 {
				delta.ArgsDelta = string(acc.step.Arguments)
				acc.args.Write(acc.step.Arguments)
				acc.step.Arguments = nil
			}
			return chat.Chunk{Role: chat.RoleAssistant, ToolCallDeltas: []chat.ToolCallDelta{delta}}, true, nil
		}
	case "step.delta":
		return s.applyDelta(ev)
	case "interaction.completed":
		return s.complete(ev.Interaction)
	case "error":
		msg := "stream error"
		if ev.Error != nil {
			msg = ev.Error.Message
		}
		return chat.Chunk{}, false, fmt.Errorf("gemini: %s: %w", msg, chat.ErrProviderUnavailable)
	}
	return chat.Chunk{}, false, nil
}

func (s *ixStream) applyDelta(ev ixEvent) (chat.Chunk, bool, error) {
	acc := s.steps[ev.Index]
	if acc == nil {
		return chat.Chunk{}, false, nil
	}
	var d ixDelta
	// An undecodable delta leaves Type empty and is skipped like any
	// unknown delta type the API may add.
	_ = json.Unmarshal(ev.Delta, &d)
	chunk := chat.Chunk{Role: chat.RoleAssistant}
	switch d.Type {
	case "text":
		acc.text.WriteString(d.Text)
		chunk.Delta = d.Text
	case "thought_summary":
		if d.Content == nil || d.Content.Type != "text" {
			return chat.Chunk{}, false, nil
		}
		acc.sum.WriteString(d.Content.Text)
		chunk.ReasoningDelta = d.Content.Text
	case "thought_signature":
		acc.step.Signature += d.Signature
		return chat.Chunk{}, false, nil
	case "arguments_delta":
		acc.args.WriteString(d.Arguments)
		chunk.ToolCallDeltas = []chat.ToolCallDelta{{Index: s.toolIndex[ev.Index], ArgsDelta: d.Arguments}}
	default:
		return chat.Chunk{}, false, nil
	}
	return chunk, true, nil
}

func (s *ixStream) complete(ix *ixInteraction) (chat.Chunk, bool, error) {
	s.finished = true
	chunk := chat.Chunk{Role: chat.RoleAssistant, Done: true}
	if ix != nil {
		if ix.Status != "" {
			s.status = ix.Status
		}
		if err := statusError(s.status, ix.Errors); err != nil {
			return chat.Chunk{}, false, err
		}
		if ix.Usage != nil {
			usage := ix.Usage.toUsage()
			chunk.Usage = &usage
		}
	}
	chunk.FinishReason = finishReason(s.status, len(s.toolIndex) > 0)
	replay := make([]json.RawMessage, 0, len(s.order))
	for _, idx := range s.order {
		replay = append(replay, s.steps[idx].raw())
	}
	if len(replay) > 0 {
		chunk.ProviderMetadata = map[string]any{"gemini": interactionsMessageOptions{InteractionSteps: replay}}
	}
	return chunk, true, nil
}
