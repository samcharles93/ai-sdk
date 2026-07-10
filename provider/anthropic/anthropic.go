package anthropic

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
	"strings"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
)

const (
	defaultBaseURL   = "https://api.anthropic.com"
	defaultTimeout   = 5 * time.Minute
	defaultMaxTokens = 4096
	anthropicVersion = "2023-06-01"
)

// --- provider-specific options ------------------------------------------

// anthropicProviderOptions holds Anthropic-specific request options
// extracted via [chat.ProviderOptionsFor] from [chat.Request.ProviderOptions].
type anthropicProviderOptions struct {
	// ReasoningEffort controls extended thinking via a symbolic effort
	// level. "none" always disables thinking (except on Fable 5/Mythos 5/
	// Mythos Preview, which can't be disabled at all). The rest of the
	// value's meaning depends on the model (see adaptiveOnlyModelPrefixes):
	//   - Adaptive-only models (Sonnet 5, Opus 4.8/4.7, Fable 5, Mythos 5,
	//     Mythos Preview): "low"/"medium"/"high"/"xhigh"/"max" map to
	//     thinking.type="adaptive" + output_config.effort. budget_tokens
	//     is not accepted at all on these models.
	//   - All other models: "low"/"medium"/"high"/"xhigh" map to a
	//     thinking.type="enabled" budget_tokens value (1024/4096/16384/
	//     32768). "max" is not a legacy-budget value and is treated the
	//     same as an unknown value below.
	// Unknown values cause thinking to be omitted from the request.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// ThinkingBudgetTokens directly sets the thinking token budget on the
	// legacy thinking.type="enabled" API. When > 0 it overrides
	// ReasoningEffort. Must be < max_tokens. Minimum 1024 (enforced by
	// the Anthropic API). Not supported on adaptive-only models (see
	// adaptiveOnlyModelPrefixes) — set ReasoningEffort instead, which
	// returns an error explaining the rename.
	ThinkingBudgetTokens int `json:"thinking_budget_tokens,omitempty"`

	// DisableSystemCache opts out of the default behaviour of marking the
	// system prompt as an ephemeral cache breakpoint. Caching is on by
	// default because the common case (a caller resending an identical
	// system prompt across turns of a session, e.g. an agent loop) makes
	// it a near-strict win: a cache write costs 1.25x normal input price
	// once, then subsequent reads of that prefix cost ~0.1x. A one-off,
	// single-turn call pays that 1.25x premium for no benefit, which is
	// what this flag is for.
	DisableSystemCache bool `json:"disable_system_cache,omitempty"`
}

// reasoningEffortBudget maps symbolic effort levels to thinking budget
// tokens, for the legacy thinking.type="enabled" API used by models that
// don't support adaptive thinking (see adaptiveOnlyModelPrefixes).
var reasoningEffortBudget = map[string]int{
	"low":    1024,
	"medium": 4096,
	"high":   16384,
	"xhigh":  32768,
}

// adaptiveEffortLevels are the valid output_config.effort values for
// adaptive thinking. "max" only exists here — the legacy budget-based API
// has no equivalent, since it's expressed as an unbounded thinking budget
// instead of a symbolic level.
var adaptiveEffortLevels = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
	"xhigh":  true,
	"max":    true,
}

// adaptiveOnlyModelPrefixes are Claude models where adaptive thinking
// (thinking.type="adaptive" + top-level output_config.effort) is the only
// supported thinking API — manual thinking.type="enabled"+budget_tokens is
// rejected outright with a 400, and temperature/top_p/top_k are rejected
// on every request regardless of whether thinking is active at all. Older
// and "deprecated-but-still-functional" models (Opus 4.6, Sonnet 4.6, and
// everything before) still accept the legacy budget_tokens API and are
// deliberately not in this list.
//
// Matched by prefix rather than exact string since dated snapshot
// variants (e.g. a future "claude-sonnet-5-20260315") would otherwise
// silently fall through to the legacy path and 400.
var adaptiveOnlyModelPrefixes = []string{
	"claude-sonnet-5",
	"claude-opus-4-8",
	"claude-opus-4-7",
	"claude-fable-5",
	"claude-mythos-5",
	"claude-mythos-preview",
}

func isAdaptiveOnlyModel(model string) bool {
	for _, prefix := range adaptiveOnlyModelPrefixes {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}

// Config configures an Anthropic Provider.
type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

// Provider is a chat.Provider backed by the Anthropic Messages API.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

var _ chat.Provider = (*Provider)(nil)

// New returns a new Anthropic Provider. APIKey is required.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("anthropic: APIKey is required: %w", chat.ErrInvalidRequest)
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
func (p *Provider) Name() string { return "anthropic" }

// --- wire types --------------------------------------------------------------

// wireContent is a single content block in the Anthropic wire format.
// It handles text, image, tool_use, tool_result, and thinking blocks.
type wireContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Source    *wireSource     `json:"source,omitempty"`
}

type wireSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type wireMessage struct {
	Role    string        `json:"role"`
	Content []wireContent `json:"content"`
}

type wireUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// usageFromWire converts Anthropic's usage accounting into [chat.Usage].
//
// Anthropic's input_tokens counts only tokens NOT read from or written to
// cache (i.e. those after the last cache breakpoint) — cache_read/
// cache_creation are reported separately and are NOT already included in
// it. This differs from OpenAI, whose prompt_tokens is a superset that
// already includes its cached_tokens subset. To keep [chat.Usage.PromptTokens]
// meaning "total prompt tokens" consistently across providers (what
// downstream cost/context-window accounting expects), the cache token
// counts are folded in here rather than left additive.
func usageFromWire(u wireUsage) chat.Usage {
	prompt := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	return chat.Usage{
		PromptTokens:        prompt,
		CompletionTokens:    u.OutputTokens,
		TotalTokens:         prompt + u.OutputTokens,
		CachedTokens:        u.CacheReadInputTokens,
		CacheCreationTokens: u.CacheCreationInputTokens,
	}
}

type wireResponse struct {
	ID         string        `json:"id"`
	Model      string        `json:"model"`
	Role       string        `json:"role"`
	Content    []wireContent `json:"content"`
	StopReason string        `json:"stop_reason"`
	Usage      wireUsage     `json:"usage"`
}

type wireToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// --- SSE streaming wire types ------------------------------------------------

type wireSSEEvent struct {
	Type         string          `json:"type"`
	Message      *wireSSEMsgData `json:"message,omitempty"`
	Index        int             `json:"index,omitempty"`
	ContentBlock *wireContent    `json:"content_block,omitempty"`
	Delta        json.RawMessage `json:"delta,omitempty"`
	Usage        *wireSSEUsage   `json:"usage,omitempty"`
}

type wireSSEMsgData struct {
	ID    string        `json:"id"`
	Model string        `json:"model"`
	Role  string        `json:"role"`
	Usage *wireSSEUsage `json:"usage,omitempty"`
}

type wireSSEUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// wireContentDelta is decoded from [wireSSEEvent.Delta] for
// content_block_delta events.
type wireContentDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

// wireMsgDelta is decoded from [wireSSEEvent.Delta] for message_delta events.
type wireMsgDelta struct {
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

// --- request building --------------------------------------------------------

func (p *Provider) buildBody(req chat.Request, stream bool) (map[string]any, []chat.Warning, error) {
	if req.Model == "" {
		return nil, nil, fmt.Errorf("anthropic: model is required: %w", chat.ErrInvalidRequest)
	}
	if len(req.Messages) == 0 {
		return nil, nil, fmt.Errorf("anthropic: at least one message is required: %w", chat.ErrInvalidRequest)
	}
	var warnings []chat.Warning
	var systemTexts []string
	msgs := make([]wireMessage, 0, len(req.Messages))

	for _, m := range req.Messages {
		switch m.Role {
		case chat.RoleSystem:
			text := m.Text()
			if text != "" {
				systemTexts = append(systemTexts, text)
			}
		case chat.RoleUser:
			content, ws := buildUserContent(m)
			warnings = append(warnings, ws...)
			if len(content) == 0 {
				content = []wireContent{{Type: "text", Text: ""}}
			}
			msgs = append(msgs, wireMessage{Role: "user", Content: content})
		case chat.RoleAssistant:
			content, ws := buildAssistantContent(m)
			warnings = append(warnings, ws...)
			if len(content) == 0 {
				content = []wireContent{{Type: "text", Text: ""}}
			}
			msgs = append(msgs, wireMessage{Role: "assistant", Content: content})
		case chat.RoleTool:
			if m.ToolCallID == "" {
				return nil, nil, fmt.Errorf("anthropic: tool message missing ToolCallID: %w", chat.ErrInvalidRequest)
			}
			msgs = append(msgs, wireMessage{
				Role: "user",
				Content: []wireContent{{
					Type:      "tool_result",
					ToolUseID: m.ToolCallID,
					Content:   m.Text(),
				}},
			})
		default:
			msgs = append(msgs, wireMessage{
				Role:    "user",
				Content: []wireContent{{Type: "text", Text: m.Text()}},
			})
		}
	}

	body := map[string]any{
		"model":    req.Model,
		"messages": msgs,
	}
	opts, _ := chat.ProviderOptionsFor[anthropicProviderOptions](req.ProviderOptions, "anthropic")
	if len(systemTexts) > 0 {
		joined := strings.Join(systemTexts, "\n")
		if opts.DisableSystemCache {
			body["system"] = joined
		} else {
			// A content-block array (vs. a plain string) is required to
			// attach cache_control. See DisableSystemCache's doc comment
			// for why this is the default.
			body["system"] = []map[string]any{
				{
					"type":          "text",
					"text":          joined,
					"cache_control": map[string]any{"type": "ephemeral"},
				},
			}
		}
	}
	explicitMaxTokens := req.MaxTokens != 0
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}

	// ensureBudgetFits reconciles a thinking budget against maxTokens. The
	// API requires budget_tokens < max_tokens (thinking tokens must leave
	// room for actual output). When the caller left MaxTokens unset, the
	// fallback default (4096) coincides with the "medium" effort budget
	// (also 4096), so a default-max-tokens request with medium effort
	// always violated that constraint. Rather than surface that as an
	// error the caller never asked for, grow the implicit max_tokens to
	// fit the budget plus a normal output allowance. An explicitly-set
	// MaxTokens that's still too small is a genuine caller conflict and
	// stays an error.
	ensureBudgetFits := func(budget int, label string) error {
		if budget < maxTokens {
			return nil
		}
		if explicitMaxTokens {
			return fmt.Errorf("anthropic: %s maps to budget_tokens %d, must be less than max_tokens (%d): %w",
				label, budget, maxTokens, chat.ErrInvalidRequest)
		}
		maxTokens = budget + defaultMaxTokens
		return nil
	}

	// --- apply thinking options ---
	adaptiveOnly := isAdaptiveOnlyModel(req.Model)
	// legacyThinkingEnabled tracks the OLD thinking.type="enabled" path
	// specifically (used only to decide whether budget/max_tokens
	// reconciliation applies below) — NOT whether thinking is active in
	// general. See dropSamplingParams for the latter.
	var legacyThinkingEnabled bool
	switch {
	case adaptiveOnly:
		if opts.ThinkingBudgetTokens > 0 {
			return nil, nil, fmt.Errorf("anthropic: thinking_budget_tokens is not supported on %s (adaptive-only thinking model); use reasoning_effort instead: %w",
				req.Model, chat.ErrInvalidRequest)
		}
		switch {
		case opts.ReasoningEffort == "none":
			body["thinking"] = map[string]any{"type": "disabled"}
		case opts.ReasoningEffort == "":
			// Leave thinking unset: on these models that means adaptive
			// thinking at its own default (Sonnet 5 defaults it on;
			// Fable 5/Mythos 5/Mythos Preview can't be turned off at
			// all). No explicit output_config.effort, so Anthropic's
			// own default ("high") applies.
		case adaptiveEffortLevels[opts.ReasoningEffort]:
			body["thinking"] = map[string]any{"type": "adaptive"}
			body["output_config"] = map[string]any{"effort": opts.ReasoningEffort}
		}
		// Unknown ReasoningEffort: silently omit thinking, same as the
		// legacy branch below.
	case opts.ThinkingBudgetTokens > 0:
		if err := ensureBudgetFits(opts.ThinkingBudgetTokens, fmt.Sprintf("thinking_budget_tokens (%d)", opts.ThinkingBudgetTokens)); err != nil {
			return nil, nil, err
		}
		body["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": opts.ThinkingBudgetTokens,
		}
		legacyThinkingEnabled = true
	case opts.ReasoningEffort != "":
		if opts.ReasoningEffort == "none" {
			body["thinking"] = map[string]any{"type": "disabled"}
		} else if budget, ok := reasoningEffortBudget[opts.ReasoningEffort]; ok {
			if err := ensureBudgetFits(budget, fmt.Sprintf("reasoning_effort %q", opts.ReasoningEffort)); err != nil {
				return nil, nil, err
			}
			body["thinking"] = map[string]any{
				"type":          "enabled",
				"budget_tokens": budget,
			}
			legacyThinkingEnabled = true
		}
		// Unknown ReasoningEffort: silently omit thinking.
	}
	// All zero/unmatched: omit thinking from body (provider default).
	body["max_tokens"] = maxTokens

	if len(req.Tools) > 0 {
		tools := make([]wireToolDef, len(req.Tools))
		for i, t := range req.Tools {
			td := wireToolDef{
				Name:        t.Name,
				Description: t.Description,
			}
			if len(t.Parameters) > 0 {
				td.InputSchema = t.Parameters
			} else {
				td.InputSchema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools[i] = td
		}
		body["tools"] = tools
	}
	if req.ToolChoice != nil {
		tc := map[string]any{}
		switch req.ToolChoice.Type {
		case chat.ToolChoiceAuto:
			tc["type"] = "auto"
		case chat.ToolChoiceNone:
			// Anthropic has no "none" tool_choice — omit tools to prevent calls.
			// If tools were in conversation history they must be replayed; we keep
			// them in messages but strip the tools definition.
			delete(body, "tools")
			if len(req.Tools) > 0 {
				warnings = append(warnings, chat.Warning{
					Type:    "unsupported-option",
					Message: "anthropic: tool_choice=none omitted tool definitions from request; tool_use blocks in conversation history may still reference past tools",
				})
			}
		case chat.ToolChoiceRequired:
			tc["type"] = "any"
		case chat.ToolChoiceTool:
			if req.ToolChoice.Name == "" {
				return nil, nil, fmt.Errorf("anthropic: tool_choice type=tool requires Name: %w", chat.ErrInvalidRequest)
			}
			tc["type"] = "tool"
			tc["name"] = req.ToolChoice.Name
		default:
			return nil, nil, fmt.Errorf("anthropic: unknown tool_choice type %q: %w", req.ToolChoice.Type, chat.ErrInvalidRequest)
		}
		if _, hasTools := body["tools"]; hasTools {
			body["tool_choice"] = tc
		}
	}
	// Anthropic rejects temperature/top_p entirely when legacy extended
	// thinking is enabled (temperature is pinned to 1 server-side).
	// Adaptive-only models reject them on EVERY request regardless of
	// whether thinking is active at all — a stricter, model-wide rule,
	// not a thinking-state-dependent one.
	dropSamplingParams := adaptiveOnly || legacyThinkingEnabled
	if dropSamplingParams {
		if req.Temperature != 0 || req.TopP != 0 {
			warnings = append(warnings, chat.Warning{
				Type:    "unsupported-option",
				Message: "anthropic: temperature/top_p are not supported on this model/mode; both omitted",
			})
		}
	} else {
		if req.Temperature != 0 {
			body["temperature"] = req.Temperature
		}
		if req.TopP != 0 {
			body["top_p"] = req.TopP
		}
	}
	if len(req.Stop) > 0 {
		body["stop_sequences"] = req.Stop
	}
	if stream {
		body["stream"] = true
	}
	return body, warnings, nil
}

// buildUserContent converts a user message's Parts into Anthropic content
// blocks. It returns any non-fatal warnings about unsupported content.
func buildUserContent(m chat.Message) ([]wireContent, []chat.Warning) {
	var blocks []wireContent
	var warnings []chat.Warning
	for _, part := range m.GetParts() {
		switch p := part.(type) {
		case chat.TextPart:
			if p.Text != "" {
				blocks = append(blocks, wireContent{Type: "text", Text: p.Text})
			}
		case chat.ImagePart:
			if len(p.Data) > 0 {
				mt := p.MediaType
				if mt == "" {
					mt = "image/png"
				}
				blocks = append(blocks, wireContent{
					Type: "image",
					Source: &wireSource{
						Type:      "base64",
						MediaType: mt,
						Data:      base64.StdEncoding.EncodeToString(p.Data),
					},
				})
			} else if p.URL != "" {
				warnings = append(warnings, chat.Warning{
					Type:    "unsupported-content",
					Message: "anthropic: URL-based images not supported; use Data (inline base64)",
				})
			}
		case chat.FilePart:
			warnings = append(warnings, chat.Warning{
				Type:    "unsupported-content",
				Message: "anthropic: FilePart not supported",
			})
		case chat.ReasoningPart:
			// ReasoningPart is not expected on user messages.
			warnings = append(warnings, chat.Warning{
				Type:    "unsupported-content",
				Message: "anthropic: ReasoningPart on user message dropped",
			})
		}
	}
	return blocks, warnings
}

// buildAssistantContent converts an assistant message's Parts and ToolCalls
// into Anthropic content blocks.
func buildAssistantContent(m chat.Message) ([]wireContent, []chat.Warning) {
	var blocks []wireContent
	var warnings []chat.Warning
	for _, part := range m.GetParts() {
		switch p := part.(type) {
		case chat.TextPart:
			if p.Text != "" {
				blocks = append(blocks, wireContent{Type: "text", Text: p.Text})
			}
		case chat.ReasoningPart:
			sig, _ := p.ProviderMetadata["anthropic:thinking_signature"].(string)
			blocks = append(blocks, wireContent{
				Type:      "thinking",
				Thinking:  p.Text,
				Signature: sig,
			})
		default:
			warnings = append(warnings, chat.Warning{
				Type:    "unsupported-content",
				Message: fmt.Sprintf("anthropic: %T on assistant message dropped", part),
			})
		}
	}
	for _, tc := range m.ToolCalls {
		var input json.RawMessage
		if strings.TrimSpace(tc.Arguments) == "" {
			input = json.RawMessage(`{}`)
		} else if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
			// Wrap non-JSON content.
			wrapped := json.RawMessage(fmt.Sprintf(`{"_content":%q}`, tc.Arguments))
			input = wrapped
		}
		blocks = append(blocks, wireContent{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Name,
			Input: input,
		})
	}
	return blocks, warnings
}

func (p *Provider) newHTTPRequest(ctx context.Context, body map[string]any) (*http.Request, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("Content-Type", "application/json")
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
	case resp.StatusCode == http.StatusBadRequest &&
		(strings.Contains(strings.ToLower(snippet), "prompt is too long") ||
			strings.Contains(strings.ToLower(snippet), "input is too long") ||
			strings.Contains(strings.ToLower(snippet), "context length")):
		base = chat.ErrContextLength
	case resp.StatusCode == http.StatusBadRequest:
		base = chat.ErrInvalidRequest
	case resp.StatusCode >= 500:
		base = chat.ErrProviderUnavailable
	default:
		base = chat.ErrProviderUnavailable
	}
	return fmt.Errorf("anthropic: status %d: %s: %w", resp.StatusCode, snippet, base)
}

func mapStopReason(r string) string {
	switch r {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		if r == "" {
			return ""
		}
		return strings.ToLower(r)
	}
}

// --- Chat (non-streaming) ----------------------------------------------------

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
		return chat.Response{}, fmt.Errorf("anthropic: http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return chat.Response{}, classifyHTTPError(resp)
	}
	var wr wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return chat.Response{}, fmt.Errorf("anthropic: decode response: %w", err)
	}
	out := chat.Response{
		ID:       wr.ID,
		Model:    wr.Model,
		Role:     chat.RoleAssistant,
		Warnings: warnings,
		Usage:    usageFromWire(wr.Usage),
	}
	var textBuf strings.Builder
	var calls []chat.ToolCall
	var parts chat.Parts
	for _, block := range wr.Content {
		switch block.Type {
		case "text":
			textBuf.WriteString(block.Text)
			parts = append(parts, chat.TextPart{Text: block.Text})
		case "tool_use":
			args := string(block.Input)
			if args == "" {
				args = "{}"
			}
			calls = append(calls, chat.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		case "thinking":
			rp := chat.ReasoningPart{Text: block.Thinking}
			if block.Signature != "" {
				rp.ProviderMetadata = map[string]any{
					"anthropic:thinking_signature": block.Signature,
				}
			}
			parts = append(parts, rp)
		}
	}
	out.Content = textBuf.String()
	out.Parts = parts
	out.ToolCalls = calls
	fr := mapStopReason(wr.StopReason)
	if len(calls) > 0 && (fr == "" || fr == "stop") {
		fr = "tool_calls"
	}
	out.FinishReason = fr
	return out, nil
}

// --- ChatStream --------------------------------------------------------------

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
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: http do: %w", err)
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
		toolBlocks:      make(map[int]*streamToolBlock),
		thinkingBlocks:  make(map[int]*streamThinkingBlock),
	}, nil
}

type streamToolBlock struct {
	id   string
	name string
	args strings.Builder
}

type streamThinkingBlock struct {
	thinking  strings.Builder
	signature strings.Builder
}

type stream struct {
	resp                *http.Response
	reader              *bufio.Reader
	closed              bool
	doneEmitted         bool
	msgID               string
	msgModel            string
	inputTokens         int
	outputTokens        int
	cachedTokens        int
	cacheCreationTokens int
	finishReason        string
	sawToolCall         bool
	pendingWarnings     []chat.Warning
	toolBlocks          map[int]*streamToolBlock
	thinkingBlocks      map[int]*streamThinkingBlock
}

// usage folds the stream's accumulated token counts into a [chat.Usage],
// applying the same cache-token-inclusion convention as usageFromWire.
func (s *stream) usage() chat.Usage {
	prompt := s.inputTokens + s.cachedTokens + s.cacheCreationTokens
	return chat.Usage{
		PromptTokens:        prompt,
		CompletionTokens:    s.outputTokens,
		TotalTokens:         prompt + s.outputTokens,
		CachedTokens:        s.cachedTokens,
		CacheCreationTokens: s.cacheCreationTokens,
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

// readSSEEvent reads the next SSE event from the stream. It returns the
// parsed wireSSEEvent, or io.EOF when the stream ends.
func (s *stream) readSSEEvent() (*wireSSEEvent, error) {
	var buf bytes.Buffer
	for {
		line, err := s.reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				if buf.Len() > 0 {
					break
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			// Skip event: lines — the "type" field in the JSON data is
			// authoritative for event discrimination.
			if _, ok := strings.CutPrefix(line, "event:"); ok {
				continue
			}
			if v, ok := strings.CutPrefix(line, "data:"); ok {
				v = strings.TrimPrefix(v, " ")
				if buf.Len() > 0 {
					buf.WriteByte('\n')
				}
				buf.WriteString(v)
				continue
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if buf.Len() > 0 {
					break
				}
				return nil, io.EOF
			}
			return nil, err
		}
	}
	data := buf.Bytes()
	var evt wireSSEEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return nil, fmt.Errorf("anthropic: decode SSE event: %w", err)
	}
	return &evt, nil
}

// Next returns the next chunk in the stream.
func (s *stream) Next(ctx context.Context) (chat.Chunk, error) {
	if err := ctx.Err(); err != nil {
		return chat.Chunk{}, err
	}
	if s.closed {
		return chat.Chunk{}, io.EOF
	}
	if s.doneEmitted {
		return chat.Chunk{}, io.EOF
	}
	for {
		if err := ctx.Err(); err != nil {
			return chat.Chunk{}, err
		}
		evt, err := s.readSSEEvent()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if !s.doneEmitted {
					s.doneEmitted = true
					usage := s.usage()
					return chat.Chunk{
						Done:  true,
						Role:  chat.RoleAssistant,
						Usage: &usage,
					}, nil
				}
				return chat.Chunk{}, io.EOF
			}
			return chat.Chunk{}, fmt.Errorf("anthropic: stream read: %w", err)
		}
		switch evt.Type {
		case "message_start":
			if evt.Message != nil {
				s.msgID = evt.Message.ID
				s.msgModel = evt.Message.Model
				if evt.Message.Usage != nil {
					s.inputTokens = evt.Message.Usage.InputTokens
					s.cachedTokens = evt.Message.Usage.CacheReadInputTokens
					s.cacheCreationTokens = evt.Message.Usage.CacheCreationInputTokens
				}
			}
		case "content_block_start":
			if evt.ContentBlock != nil {
				idx := evt.Index
				switch evt.ContentBlock.Type {
				case "tool_use":
					s.toolBlocks[idx] = &streamToolBlock{
						id:   evt.ContentBlock.ID,
						name: evt.ContentBlock.Name,
					}
				case "thinking":
					s.thinkingBlocks[idx] = &streamThinkingBlock{}
				}
			}
		case "content_block_delta":
			idx := evt.Index
			var d wireContentDelta
			if err := json.Unmarshal(evt.Delta, &d); err != nil {
				return chat.Chunk{}, fmt.Errorf("anthropic: decode content_block_delta: %w", err)
			}
			switch d.Type {
			case "text_delta":
				out := chat.Chunk{
					Delta: d.Text,
					Role:  chat.RoleAssistant,
				}
				if len(s.pendingWarnings) > 0 {
					out.Warnings = s.pendingWarnings
					s.pendingWarnings = nil
				}
				return out, nil
			case "input_json_delta":
				tb := s.toolBlocks[idx]
				if tb == nil {
					continue
				}
				tb.args.WriteString(d.PartialJSON)
				out := chat.Chunk{
					Role: chat.RoleAssistant,
					ToolCallDeltas: []chat.ToolCallDelta{{
						Index:     idx,
						ID:        tb.id,
						Name:      tb.name,
						ArgsDelta: d.PartialJSON,
					}},
				}
				if len(s.pendingWarnings) > 0 {
					out.Warnings = s.pendingWarnings
					s.pendingWarnings = nil
				}
				return out, nil
			case "thinking_delta":
				tb := s.thinkingBlocks[idx]
				if tb != nil {
					tb.thinking.WriteString(d.Thinking)
				}
				out := chat.Chunk{
					ReasoningDelta: d.Thinking,
					Role:           chat.RoleAssistant,
				}
				if len(s.pendingWarnings) > 0 {
					out.Warnings = s.pendingWarnings
					s.pendingWarnings = nil
				}
				return out, nil
			case "signature_delta":
				tb := s.thinkingBlocks[idx]
				if tb != nil {
					tb.signature.WriteString(d.Signature)
				}
				continue
			}
		case "content_block_stop":
			// Mark block as complete; no chunk emitted.
		case "message_delta":
			var md wireMsgDelta
			if err := json.Unmarshal(evt.Delta, &md); err != nil {
				return chat.Chunk{}, fmt.Errorf("anthropic: decode message_delta: %w", err)
			}
			s.finishReason = md.StopReason
			if evt.Usage != nil {
				s.outputTokens = evt.Usage.OutputTokens
				// message_delta's usage is an output-side update in
				// practice, but pick up cache fields defensively in case
				// a future API revision includes them here too.
				if evt.Usage.CacheReadInputTokens > 0 {
					s.cachedTokens = evt.Usage.CacheReadInputTokens
				}
				if evt.Usage.CacheCreationInputTokens > 0 {
					s.cacheCreationTokens = evt.Usage.CacheCreationInputTokens
				}
			}
		case "message_stop":
			s.doneEmitted = true
			fr := mapStopReason(s.finishReason)
			if s.sawToolCall && (fr == "" || fr == "stop") {
				fr = "tool_calls"
			}
			usage := s.usage()
			out := chat.Chunk{
				Done:         true,
				Role:         chat.RoleAssistant,
				FinishReason: fr,
				Usage:        &usage,
			}
			if len(s.pendingWarnings) > 0 {
				out.Warnings = s.pendingWarnings
				s.pendingWarnings = nil
			}
			return out, nil
		case "ping":
			continue
		}
	}
}
