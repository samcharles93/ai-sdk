// Package ollama provides a chat.Provider implementation backed by an
// Ollama HTTP server (https://ollama.com).
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
)

const defaultBaseURL = "http://localhost:11434"

// Config configures a Provider.
type Config struct {
	// BaseURL is the root URL of the Ollama server. If empty,
	// "http://localhost:11434" is used.
	BaseURL string
	// HTTPClient is used for all requests. If nil, a client with a
	// 5-minute timeout is used.
	HTTPClient *http.Client
}

// Provider is a chat.Provider backed by an Ollama server.
type Provider struct {
	baseURL string
	http    *http.Client
}

// New constructs a Provider from cfg, applying defaults for unset fields.
func New(cfg Config) *Provider {
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Provider{baseURL: base, http: hc}
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "ollama" }

// ollamaMessage mirrors the wire shape used by Ollama's /api/chat for
// both request and response. ToolCalls carries assistant tool invocations;
// arguments is an OBJECT on the wire (json.RawMessage), not a string as in
// the OpenAI family.
//
// Note: Ollama does not use a tool_call_id field for tool results — tool
// results are matched positionally to the most recent assistant tool_calls
// block. chat.Message.ToolCallID is therefore preserved on the SDK side
// but deliberately NOT serialised here.
type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Thinking  string           `json:"thinking,omitempty"`
	Images    []string         `json:"images,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function ollamaToolCallFunction `json:"function"`
}

type ollamaToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type ollamaOptions struct {
	Temperature float32  `json:"temperature,omitempty"`
	TopP        float32  `json:"top_p,omitempty"`
	NumPredict  int      `json:"num_predict,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  *ollamaOptions  `json:"options,omitempty"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	Think    bool            `json:"think,omitempty"`
}

// ollamaProviderOptions carries ollama-specific request options, set via
// [chat.Request.ProviderOptions] keyed by "ollama".
type ollamaProviderOptions struct {
	// ReasoningEffort, when set to anything other than "none", opts the
	// request into Ollama's "think" mode so thinking-capable models
	// (deepseek-r1, qwen3, kimi-k2 ...) return message.thinking deltas.
	// Ollama's think field is a boolean, so any non-"none" effort level
	// maps to true; effort granularity is not supported server-side.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type ollamaResponse struct {
	Model           string        `json:"model"`
	CreatedAt       string        `json:"created_at"`
	Message         ollamaMessage `json:"message"`
	Done            bool          `json:"done"`
	DoneReason      string        `json:"done_reason"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
}

// buildRequestBody builds the wire request and a list of non-fatal
// warnings about parts of the input that this provider could not
// faithfully transmit (e.g. unsupported FilePart/ReasoningPart on input,
// or remote-URL ImageParts which Ollama cannot fetch directly).
func buildRequestBody(req chat.Request, stream bool) (ollamaRequest, []chat.Warning) {
	var warnings []chat.Warning
	msgs := make([]ollamaMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		om := ollamaMessage{Role: string(m.Role)}
		// Iterate canonical Parts; GetParts auto-promotes Content when
		// no Parts are set so legacy callers still work.
		var textChunks []string
		for _, part := range m.GetParts() {
			switch p := part.(type) {
			case chat.TextPart:
				if p.Text != "" {
					textChunks = append(textChunks, p.Text)
				}
			case chat.ImagePart:
				if len(p.Data) > 0 {
					om.Images = append(om.Images, base64.StdEncoding.EncodeToString(p.Data))
				} else {
					// Ollama expects base64 inline; URL-only is unsupported.
					warnings = append(warnings, chat.Warning{
						Type:    "unsupported-content",
						Message: "ollama: ImagePart with URL is not supported; pass image bytes via Data",
					})
				}
			case chat.FilePart:
				warnings = append(warnings, chat.Warning{
					Type:    "unsupported-content",
					Message: "ollama: FilePart is not supported on input",
				})
			case chat.ReasoningPart:
				// Replay assistant reasoning into the wire `thinking` field
				// for assistant turns. For other roles, drop with no warning
				// (this would only happen via misuse).
				if m.Role == chat.RoleAssistant && p.Text != "" {
					if om.Thinking != "" {
						om.Thinking += p.Text
					} else {
						om.Thinking = p.Text
					}
				}
			}
		}
		if len(textChunks) > 0 {
			om.Content = strings.Join(textChunks, "")
		}
		if m.Role == chat.RoleAssistant && len(m.ToolCalls) > 0 {
			om.ToolCalls = make([]ollamaToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				args := json.RawMessage(tc.Arguments)
				if len(args) == 0 {
					args = json.RawMessage(`{}`)
				}
				om.ToolCalls = append(om.ToolCalls, ollamaToolCall{
					Function: ollamaToolCallFunction{Name: tc.Name, Arguments: args},
				})
			}
		}
		// RoleTool: chat.Message.ToolCallID is intentionally NOT propagated
		// to the wire — Ollama does not use tool_call_id and matches results
		// positionally against the prior assistant tool_calls block.
		msgs = append(msgs, om)
	}
	body := ollamaRequest{Model: req.Model, Messages: msgs, Stream: stream}
	if opts, _ := chat.ProviderOptionsFor[ollamaProviderOptions](req.ProviderOptions, "ollama"); opts.ReasoningEffort != "" && opts.ReasoningEffort != "none" {
		body.Think = true
	}
	if len(req.Tools) > 0 {
		body.Tools = make([]ollamaTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, ollamaTool{
				Type: "function",
				Function: ollamaToolFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			})
		}
	}
	// req.ToolChoice is silently ignored: Ollama's /api/chat does not
	// currently document a tool_choice field, so passing one would be a
	// no-op or an error depending on server version. Drop it cleanly.
	_ = req.ToolChoice
	var opts ollamaOptions
	set := false
	if req.Temperature != 0 {
		opts.Temperature = req.Temperature
		set = true
	}
	if req.TopP != 0 {
		opts.TopP = req.TopP
		set = true
	}
	if req.MaxTokens > 0 {
		opts.NumPredict = req.MaxTokens
		set = true
	}
	if len(req.Stop) > 0 {
		opts.Stop = req.Stop
		set = true
	}
	if set {
		body.Options = &opts
	}
	return body, warnings
}

func (p *Provider) doRequest(ctx context.Context, body ollamaRequest) (*http.Response, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson, application/json")
	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: http call: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		base := classifyStatus(resp.StatusCode)
		return nil, fmt.Errorf("ollama: http %d: %s: %w", resp.StatusCode, chat.SanitizeErrorBody(snippet), base)
	}
	return resp, nil
}

func classifyStatus(code int) error {
	switch {
	case code == 401 || code == 403:
		return chat.ErrAuthFailed
	case code == 429:
		return chat.ErrRateLimited
	case code >= 500:
		return chat.ErrProviderUnavailable
	default:
		return chat.ErrProviderUnavailable
	}
}

// Chat performs a non-streaming chat completion against Ollama's /api/chat.
func (p *Provider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	if req.Model == "" {
		return chat.Response{}, fmt.Errorf("ollama: model is required: %w", chat.ErrInvalidRequest)
	}
	body, warnings := buildRequestBody(req, false)
	resp, err := p.doRequest(ctx, body)
	if err != nil {
		return chat.Response{}, err
	}
	defer resp.Body.Close()

	var or ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&or); err != nil {
		return chat.Response{}, fmt.Errorf("ollama: decode response: %w", err)
	}
	finish := or.DoneReason
	if finish == "" {
		finish = "stop"
	}
	var toolCalls []chat.ToolCall
	if len(or.Message.ToolCalls) > 0 {
		toolCalls = make([]chat.ToolCall, 0, len(or.Message.ToolCalls))
		for i, tc := range or.Message.ToolCalls {
			args := string(tc.Function.Arguments)
			if args == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, chat.ToolCall{
				ID:        fmt.Sprintf("call_%d", i),
				Name:      tc.Function.Name,
				Arguments: args,
			})
		}
		if finish == "" || finish == "stop" {
			finish = "tool_calls"
		}
	}
	// Build canonical Parts: reasoning first (when present), then text.
	var parts chat.Parts
	if or.Message.Thinking != "" {
		parts = append(parts, chat.ReasoningPart{Text: or.Message.Thinking})
	}
	if or.Message.Content != "" {
		parts = append(parts, chat.TextPart{Text: or.Message.Content})
	}
	return chat.Response{
		Model:        or.Model,
		Role:         chat.RoleAssistant,
		Content:      or.Message.Content,
		Parts:        parts,
		ToolCalls:    toolCalls,
		FinishReason: finish,
		Warnings:     warnings,
		Usage: chat.Usage{
			PromptTokens:     or.PromptEvalCount,
			CompletionTokens: or.EvalCount,
			TotalTokens:      or.PromptEvalCount + or.EvalCount,
		},
	}, nil
}

// ChatStream performs a streaming chat completion. The returned Stream
// yields one Chunk per NDJSON line emitted by Ollama.
func (p *Provider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	if req.Model == "" {
		return nil, fmt.Errorf("ollama: model is required: %w", chat.ErrInvalidRequest)
	}
	body, warnings := buildRequestBody(req, true)
	resp, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	return &stream{resp: resp, scanner: sc, pendingWarnings: warnings}, nil
}

type stream struct {
	resp            *http.Response
	scanner         *bufio.Scanner
	closed          bool
	done            bool
	sawToolCalls    bool
	pendingWarnings []chat.Warning
}

func (s *stream) Next(ctx context.Context) (chat.Chunk, error) {
	if err := ctx.Err(); err != nil {
		return chat.Chunk{}, err
	}
	if s.closed || s.done {
		return chat.Chunk{}, io.EOF
	}
	for {
		if !s.scanner.Scan() {
			if err := s.scanner.Err(); err != nil {
				return chat.Chunk{}, fmt.Errorf("ollama: stream read: %w", err)
			}
			return chat.Chunk{}, io.EOF
		}
		line := bytes.TrimSpace(s.scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var or ollamaResponse
		if err := json.Unmarshal(line, &or); err != nil {
			return chat.Chunk{}, fmt.Errorf("ollama: decode stream chunk: %w", err)
		}
		chunk := chat.Chunk{
			Delta:          or.Message.Content,
			ReasoningDelta: or.Message.Thinking,
			Role:           chat.RoleAssistant,
		}
		// Flush request-time warnings on the first chunk so they reach
		// the consumer once.
		if len(s.pendingWarnings) > 0 {
			chunk.Warnings = s.pendingWarnings
			s.pendingWarnings = nil
		}
		if len(or.Message.ToolCalls) > 0 {
			s.sawToolCalls = true
			deltas := make([]chat.ToolCallDelta, 0, len(or.Message.ToolCalls))
			for i, tc := range or.Message.ToolCalls {
				args := string(tc.Function.Arguments)
				if args == "" {
					args = "{}"
				}
				deltas = append(deltas, chat.ToolCallDelta{
					Index:     i,
					ID:        fmt.Sprintf("call_%d", i),
					Name:      tc.Function.Name,
					ArgsDelta: args,
				})
			}
			chunk.ToolCallDeltas = deltas
		}
		if or.Done {
			finish := or.DoneReason
			if finish == "" {
				finish = "stop"
			}
			if s.sawToolCalls && (finish == "" || finish == "stop") {
				finish = "tool_calls"
			}
			chunk.Done = true
			chunk.FinishReason = finish
			chunk.Usage = &chat.Usage{
				PromptTokens:     or.PromptEvalCount,
				CompletionTokens: or.EvalCount,
				TotalTokens:      or.PromptEvalCount + or.EvalCount,
			}
			s.done = true
		}
		return chunk, nil
	}
}

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

var _ chat.Provider = (*Provider)(nil)
