// Package gemini implements the chat.Provider interface against Google's
// Gemini native generateContent API.
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

// defaultBaseURL is the production Gemini API host.
const defaultBaseURL = "https://generativelanguage.googleapis.com"

// Config configures a Gemini Provider.
type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

// Provider implements chat.Provider for the Gemini native API.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// New constructs a Provider. APIKey is required.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("gemini: APIKey is required: %w", chat.ErrAuthFailed)
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Provider{apiKey: cfg.APIKey, baseURL: base, client: hc}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "gemini" }

// Compile-time assertion that *Provider satisfies chat.Provider.
var _ chat.Provider = (*Provider)(nil)

// --- wire types -------------------------------------------------------------

// wireFunctionCall is the Gemini representation of a model-issued tool call.
// args is an OBJECT (not a string) on the wire.
type wireFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// wireFunctionResponse is the Gemini representation of a tool result fed back
// to the model. Note that on the wire this rides under role "user", not "tool".
type wireFunctionResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

// wirePart is one element of a contents[].parts array. Exactly one of Text,
// InlineData, FileData, FunctionCall, or FunctionResponse is set per part on
// the wire; we use pointers + omitempty so json encoding produces the right
// shape.
type wirePart struct {
	Text             string                `json:"text,omitempty"`
	InlineData       *wireInlineData       `json:"inlineData,omitempty"`
	FileData         *wireFileData         `json:"fileData,omitempty"`
	FunctionCall     *wireFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *wireFunctionResponse `json:"functionResponse,omitempty"`
}

// wireInlineData carries base64-encoded multimodal bytes inline.
type wireInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// wireFileData references a previously-uploaded file by URI.
type wireFileData struct {
	MimeType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri"`
}

type wireContent struct {
	Role  string     `json:"role,omitempty"`
	Parts []wirePart `json:"parts"`
}

type wireCandidate struct {
	Content      wireContent `json:"content"`
	FinishReason string      `json:"finishReason,omitempty"`
}

type wireUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type wireResponse struct {
	Candidates    []wireCandidate `json:"candidates"`
	UsageMetadata *wireUsage      `json:"usageMetadata,omitempty"`
}

// --- helpers ---------------------------------------------------------------

func mapFinishReason(r string) string {
	switch r {
	case "":
		return ""
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "safety"
	case "RECITATION":
		return "recitation"
	default:
		return strings.ToLower(r)
	}
}

// partsFromMessage converts a chat.Message's canonical Parts into wire
// parts for Gemini. It returns the wire parts, any plain-text fallback
// (used by RoleSystem which is collected separately), and a list of
// non-fatal warnings about content this provider could not encode.
func partsFromMessage(m chat.Message) (wireParts []wirePart, textOnly string, warnings []chat.Warning) {
	var sb strings.Builder
	for _, part := range m.GetParts() {
		switch p := part.(type) {
		case chat.TextPart:
			if p.Text != "" {
				wireParts = append(wireParts, wirePart{Text: p.Text})
				sb.WriteString(p.Text)
			}
		case chat.ImagePart:
			if len(p.Data) > 0 {
				mt := p.MediaType
				if mt == "" {
					mt = "image/png"
				}
				wireParts = append(wireParts, wirePart{
					InlineData: &wireInlineData{
						MimeType: mt,
						Data:     base64.StdEncoding.EncodeToString(p.Data),
					},
				})
			} else if p.URL != "" {
				wireParts = append(wireParts, wirePart{
					FileData: &wireFileData{MimeType: p.MediaType, FileURI: p.URL},
				})
			}
		case chat.FilePart:
			if len(p.Data) > 0 {
				wireParts = append(wireParts, wirePart{
					InlineData: &wireInlineData{
						MimeType: p.MediaType,
						Data:     base64.StdEncoding.EncodeToString(p.Data),
					},
				})
			} else if p.URL != "" {
				wireParts = append(wireParts, wirePart{
					FileData: &wireFileData{MimeType: p.MediaType, FileURI: p.URL},
				})
			}
		case chat.ReasoningPart:
			// Gemini does not accept reasoning replay on the request path.
			// Drop silently for assistant turns (the API will not re-process
			// thinking blocks anyway); warn for non-assistant misuse.
			if m.Role != chat.RoleAssistant {
				warnings = append(warnings, chat.Warning{
					Type:    "unsupported-content",
					Message: "gemini: ReasoningPart not supported on non-assistant role",
				})
			}
		}
	}
	textOnly = sb.String()
	return
}

func buildBody(req chat.Request) ([]byte, []chat.Warning, error) {
	var systemTexts []string
	var allWarnings []chat.Warning
	contents := make([]wireContent, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case chat.RoleSystem:
			_, textOnly, ws := partsFromMessage(m)
			allWarnings = append(allWarnings, ws...)
			if textOnly == "" && m.Content != "" {
				textOnly = m.Content
			}
			if textOnly != "" {
				systemTexts = append(systemTexts, textOnly)
			}
		case chat.RoleUser:
			parts, _, ws := partsFromMessage(m)
			allWarnings = append(allWarnings, ws...)
			if len(parts) == 0 {
				parts = []wirePart{{Text: ""}}
			}
			contents = append(contents, wireContent{Role: "user", Parts: parts})
		case chat.RoleAssistant:
			parts, _, ws := partsFromMessage(m)
			allWarnings = append(allWarnings, ws...)
			for _, tc := range m.ToolCalls {
				var args json.RawMessage
				if strings.TrimSpace(tc.Arguments) == "" {
					args = json.RawMessage(`{}`)
				} else {
					if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
						return nil, nil, fmt.Errorf("gemini: assistant tool call %q has invalid JSON arguments: %w", tc.Name, chat.ErrInvalidRequest)
					}
				}
				parts = append(parts, wirePart{
					FunctionCall: &wireFunctionCall{Name: tc.Name, Args: args},
				})
			}
			if len(parts) == 0 {
				parts = []wirePart{{Text: ""}}
			}
			contents = append(contents, wireContent{Role: "model", Parts: parts})
		case chat.RoleTool:
			// Gemini quirk: tool results ride under role "user" with a
			// functionResponse part. The function name is required; we use
			// msg.Name when set, else fall back to msg.ToolCallID (the latter
			// is a best-effort fallback because Gemini does not use call IDs).
			name := m.Name
			if name == "" {
				name = m.ToolCallID
			}
			var resp json.RawMessage
			if strings.TrimSpace(m.Content) == "" {
				resp = json.RawMessage(`{}`)
			} else if err := json.Unmarshal([]byte(m.Content), &resp); err != nil {
				// Not valid JSON — wrap as {"output": <content>}.
				wrapped, mErr := json.Marshal(map[string]string{"output": m.Content})
				if mErr != nil {
					return nil, nil, fmt.Errorf("gemini: wrap tool result: %w", mErr)
				}
				resp = json.RawMessage(wrapped)
			}
			contents = append(contents, wireContent{
				Role: "user",
				Parts: []wirePart{{
					FunctionResponse: &wireFunctionResponse{Name: name, Response: resp},
				}},
			})
		default:
			contents = append(contents, wireContent{
				Role:  "user",
				Parts: []wirePart{{Text: m.Content}},
			})
		}
	}

	body := map[string]any{
		"contents": contents,
	}
	if len(systemTexts) > 0 {
		body["systemInstruction"] = wireContent{
			Parts: []wirePart{{Text: strings.Join(systemTexts, "\n")}},
		}
	}

	if len(req.Tools) > 0 {
		decls := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			d := map[string]any{"name": t.Name}
			if t.Description != "" {
				d["description"] = t.Description
			}
			if len(t.Parameters) > 0 {
				d["parameters"] = json.RawMessage(t.Parameters)
			}
			decls = append(decls, d)
		}
		body["tools"] = []map[string]any{{"functionDeclarations": decls}}
	}

	if req.ToolChoice != nil {
		fcc := map[string]any{}
		switch req.ToolChoice.Type {
		case chat.ToolChoiceAuto:
			fcc["mode"] = "AUTO"
		case chat.ToolChoiceNone:
			fcc["mode"] = "NONE"
		case chat.ToolChoiceRequired:
			fcc["mode"] = "ANY"
		case chat.ToolChoiceTool:
			if req.ToolChoice.Name == "" {
				return nil, nil, fmt.Errorf("gemini: ToolChoiceTool requires Name: %w", chat.ErrInvalidRequest)
			}
			fcc["mode"] = "ANY"
			fcc["allowedFunctionNames"] = []string{req.ToolChoice.Name}
		default:
			return nil, nil, fmt.Errorf("gemini: unknown ToolChoice type %q: %w", req.ToolChoice.Type, chat.ErrInvalidRequest)
		}
		body["toolConfig"] = map[string]any{"functionCallingConfig": fcc}
	}

	gen := map[string]any{}
	if req.Temperature != 0 {
		gen["temperature"] = req.Temperature
	}
	if req.MaxTokens != 0 {
		gen["maxOutputTokens"] = req.MaxTokens
	}
	if req.TopP != 0 {
		gen["topP"] = req.TopP
	}
	if len(req.Stop) > 0 {
		gen["stopSequences"] = req.Stop
	}
	if len(gen) > 0 {
		body["generationConfig"] = gen
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return nil, nil, err
	}
	return buf, allWarnings, nil
}

// buildURL constructs the request URL with the model path-segment encoded and
// the API key query-escaped. The returned URL contains the API key, so it must
// not be logged.
func (p *Provider) buildURL(model, action string) string {
	// Use net/url to safely escape the model path segment.
	path := "/v1beta/models/" + url.PathEscape(model) + ":" + action
	q := "key=" + url.QueryEscape(p.apiKey)
	if action == "streamGenerateContent" {
		q = "alt=sse&" + q
	}
	return p.baseURL + path + "?" + q
}

// classifyHTTP maps a non-2xx HTTP response into a sentinel chat error,
// stripping the API key from any echoed URL in the snippet.
func classifyHTTP(code int, body []byte) error {
	snippet := chat.SanitizeErrorBody(body)
	// Defensive: scrub anything that looks like an api key echo.
	snippet = scrubKey(snippet)

	var base error
	switch {
	case code == 401 || code == 403:
		base = chat.ErrAuthFailed
	case code == 400 && strings.Contains(strings.ToLower(snippet), "api key"):
		base = chat.ErrAuthFailed
	case code == 429:
		base = chat.ErrRateLimited
	case code >= 500:
		base = chat.ErrProviderUnavailable
	default:
		base = chat.ErrProviderUnavailable
	}
	return fmt.Errorf("gemini: status %d: %s: %w", code, snippet, base)
}

func scrubKey(s string) string {
	// Best-effort: strip "key=..." query params from any URLs in error bodies.
	const repl = "key=REDACTED"
	start := 0
	for {
		rel := strings.Index(s[start:], "key=")
		if rel < 0 {
			return s
		}
		i := start + rel
		j := i + 4
		for j < len(s) && s[j] != '&' && s[j] != ' ' && s[j] != '"' && s[j] != '\n' {
			j++
		}
		s = s[:i] + repl + s[j:]
		start = i + len(repl)
	}
}

// --- non-streaming ---------------------------------------------------------

// Chat performs a non-streaming chat completion.
func (p *Provider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	if strings.TrimSpace(req.Model) == "" {
		return chat.Response{}, fmt.Errorf("gemini: model is required: %w", chat.ErrInvalidRequest)
	}
	if len(req.Messages) == 0 {
		return chat.Response{}, fmt.Errorf("gemini: at least one message is required: %w", chat.ErrInvalidRequest)
	}

	body, warnings, err := buildBody(req)
	if err != nil {
		return chat.Response{}, fmt.Errorf("gemini: marshal request: %w", err)
	}

	endpoint := p.buildURL(req.Model, "generateContent")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return chat.Response{}, fmt.Errorf("gemini: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return chat.Response{}, fmt.Errorf("gemini: do request: %w", errors.Join(err, chat.ErrProviderUnavailable))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return chat.Response{}, classifyHTTP(resp.StatusCode, raw)
	}

	var wr wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return chat.Response{}, fmt.Errorf("gemini: decode response: %w", err)
	}

	out := chat.Response{
		Model:    req.Model,
		Role:     chat.RoleAssistant,
		Warnings: warnings,
	}
	if len(wr.Candidates) > 0 {
		c := wr.Candidates[0]
		var sb strings.Builder
		var calls []chat.ToolCall
		var parts chat.Parts
		for _, part := range c.Content.Parts {
			if part.FunctionCall != nil {
				args := string(part.FunctionCall.Args)
				if args == "" {
					args = "{}"
				}
				calls = append(calls, chat.ToolCall{
					ID:        fmt.Sprintf("call_%d", len(calls)),
					Name:      part.FunctionCall.Name,
					Arguments: args,
				})
				continue
			}
			if part.Text != "" {
				sb.WriteString(part.Text)
				parts = append(parts, chat.TextPart{Text: part.Text})
			}
			if part.InlineData != nil && part.InlineData.Data != "" {
				if data, derr := base64.StdEncoding.DecodeString(part.InlineData.Data); derr == nil {
					parts = append(parts, chat.ImagePart{
						Data:      data,
						MediaType: part.InlineData.MimeType,
					})
				}
			}
		}
		out.Content = sb.String()
		out.Parts = parts
		out.ToolCalls = calls
		out.FinishReason = mapFinishReason(c.FinishReason)
		if len(calls) > 0 && (out.FinishReason == "stop" || out.FinishReason == "") {
			out.FinishReason = "tool_calls"
		}
	}
	if wr.UsageMetadata != nil {
		out.Usage = chat.Usage{
			PromptTokens:     wr.UsageMetadata.PromptTokenCount,
			CompletionTokens: wr.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      wr.UsageMetadata.TotalTokenCount,
		}
	}
	return out, nil
}

// --- streaming -------------------------------------------------------------

// ChatStream performs a streaming chat completion.
func (p *Provider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	if strings.TrimSpace(req.Model) == "" {
		return nil, fmt.Errorf("gemini: model is required: %w", chat.ErrInvalidRequest)
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("gemini: at least one message is required: %w", chat.ErrInvalidRequest)
	}

	body, warnings, err := buildBody(req)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal request: %w", err)
	}

	endpoint := p.buildURL(req.Model, "streamGenerateContent")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: do request: %w", errors.Join(err, chat.ErrProviderUnavailable))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, classifyHTTP(resp.StatusCode, raw)
	}

	return &stream{
		resp:            resp,
		reader:          bufio.NewReader(resp.Body),
		model:           req.Model,
		pendingWarnings: warnings,
	}, nil
}

type stream struct {
	resp            *http.Response
	reader          *bufio.Reader
	closed          bool
	pendingUsage    *chat.Usage
	pendingWarnings []chat.Warning
	model           string
	doneEmitted     bool
	eofReturned     bool
	toolCallIndex   int
	sawToolCall     bool
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

// readDataEvent reads the next SSE event and returns its concatenated `data:`
// JSON payload, or io.EOF when the stream ends. Comments and blank lines
// outside of an event are skipped.
func (s *stream) readDataEvent() ([]byte, error) {
	var buf bytes.Buffer
	for {
		line, err := s.reader.ReadString('\n')
		if len(line) > 0 {
			// Strip trailing CR/LF.
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				if buf.Len() > 0 {
					return buf.Bytes(), nil
				}
				// blank separator with no data; keep reading
			} else if strings.HasPrefix(line, ":") {
				// SSE comment, ignore
			} else if after, ok := strings.CutPrefix(line, "data:"); ok {
				v := after
				v = strings.TrimPrefix(v, " ")
				if buf.Len() > 0 {
					buf.WriteByte('\n')
				}
				buf.WriteString(v)
			}
		}
		if err != nil {
			if err == io.EOF {
				if buf.Len() > 0 {
					return buf.Bytes(), nil
				}
				return nil, io.EOF
			}
			return nil, err
		}
	}
}

// Next returns the next streaming chunk.
func (s *stream) Next(ctx context.Context) (chat.Chunk, error) {
	if err := ctx.Err(); err != nil {
		return chat.Chunk{}, err
	}
	if s.eofReturned {
		return chat.Chunk{}, io.EOF
	}

	for {
		if err := ctx.Err(); err != nil {
			return chat.Chunk{}, err
		}

		data, err := s.readDataEvent()
		if err != nil {
			if err == io.EOF {
				if !s.doneEmitted {
					s.doneEmitted = true
					// Don't emit io.EOF yet; emit a synthetic Done chunk first.
					return chat.Chunk{
						Done:  true,
						Role:  chat.RoleAssistant,
						Usage: s.pendingUsage,
					}, nil
				}
				s.eofReturned = true
				return chat.Chunk{}, io.EOF
			}
			return chat.Chunk{}, fmt.Errorf("gemini: read stream: %w", err)
		}

		var wr wireResponse
		if err := json.Unmarshal(data, &wr); err != nil {
			// Skip malformed chunks rather than failing the stream.
			continue
		}

		var delta strings.Builder
		var toolDeltas []chat.ToolCallDelta
		var finish string
		if len(wr.Candidates) > 0 {
			c := wr.Candidates[0]
			for _, part := range c.Content.Parts {
				if part.FunctionCall != nil {
					args := string(part.FunctionCall.Args)
					if args == "" {
						args = "{}"
					}
					idx := s.toolCallIndex
					s.toolCallIndex++
					s.sawToolCall = true
					toolDeltas = append(toolDeltas, chat.ToolCallDelta{
						Index:     idx,
						ID:        fmt.Sprintf("call_%d", idx),
						Name:      part.FunctionCall.Name,
						ArgsDelta: args,
					})
					continue
				}
				delta.WriteString(part.Text)
			}
			finish = c.FinishReason
		}

		if wr.UsageMetadata != nil {
			s.pendingUsage = &chat.Usage{
				PromptTokens:     wr.UsageMetadata.PromptTokenCount,
				CompletionTokens: wr.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      wr.UsageMetadata.TotalTokenCount,
			}
		}

		if finish != "" {
			s.doneEmitted = true
			fr := mapFinishReason(finish)
			if s.sawToolCall && (fr == "stop" || fr == "") {
				fr = "tool_calls"
			}
			out := chat.Chunk{
				Delta:          delta.String(),
				Role:           chat.RoleAssistant,
				ToolCallDeltas: toolDeltas,
				FinishReason:   fr,
				Usage:          s.pendingUsage,
				Done:           true,
			}
			if len(s.pendingWarnings) > 0 {
				out.Warnings = s.pendingWarnings
				s.pendingWarnings = nil
			}
			return out, nil
		}

		if delta.Len() == 0 && len(toolDeltas) == 0 {
			// Pure usage-only or empty chunk; loop and read the next one.
			continue
		}

		out := chat.Chunk{
			Delta:          delta.String(),
			Role:           chat.RoleAssistant,
			ToolCallDeltas: toolDeltas,
		}
		if len(s.pendingWarnings) > 0 {
			out.Warnings = s.pendingWarnings
			s.pendingWarnings = nil
		}
		return out, nil
	}
}
