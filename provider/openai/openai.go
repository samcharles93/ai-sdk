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
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
)

const (
	defaultBaseURL = "https://api.openai.com"
	defaultTimeout = 5 * time.Minute
)

type Config struct {
	// APIKey authenticates requests to OpenAI.
	APIKey string
	// BaseURL overrides the API root.
	BaseURL string
	// HTTPClient overrides the default five-minute client.
	HTTPClient *http.Client
}

// Provider implements chat.Provider over OpenAI wire protocols.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

var _ chat.Provider = (*Provider)(nil)

type wireAPI interface {
	path() string
	buildBody(chat.Request, bool) (map[string]any, []chat.Warning, error)
	decodeResponse(io.Reader, []chat.Warning) (chat.Response, error)
	parseStreamEvent([]byte) (chat.Chunk, bool, error)
}

// New constructs an OpenAI provider.
func New(cfg Config) (*Provider, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	if cfg.APIKey == "" && base == defaultBaseURL {
		return nil, fmt.Errorf("openai: APIKey is required for api.openai.com: %w", chat.ErrInvalidRequest)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &Provider{apiKey: cfg.APIKey, baseURL: normaliseBaseURL(base), client: client}, nil
}

func normaliseBaseURL(base string) string {
	base = strings.TrimRight(base, "/")
	parsed, err := url.Parse(base)
	if err != nil {
		return base
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return base + "/v1"
	}
	return base
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "openai" }

type openaiProviderOptions struct {
	ReasoningEffort  string `json:"reasoning_effort,omitempty"`
	ReasoningSummary string `json:"reasoning_summary,omitempty"`
}

func selectWireAPI(req chat.Request) wireAPI {
	options, _ := chat.ProviderOptionsFor[openaiProviderOptions](req.ProviderOptions, "openai")
	if len(req.Tools) > 0 && options.ReasoningEffort != "" && options.ReasoningEffort != "none" {
		return responsesAPI{}
	}
	return chatCompletionsAPI{}
}

func validateRequest(req chat.Request) error {
	if req.Model == "" {
		return fmt.Errorf("openai: model is required: %w", chat.ErrInvalidRequest)
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("openai: at least one message is required: %w", chat.ErrInvalidRequest)
	}
	return nil
}

func (p *Provider) newHTTPRequest(ctx context.Context, api wireAPI, body map[string]any) (*http.Request, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal %s request: %w", api.path(), err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+api.path(), bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("openai: build %s request: %w", api.path(), err)
	}
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if headers, ok := chat.ContextHeaders(ctx); ok {
		for key, value := range headers {
			request.Header.Set(key, value)
		}
	}
	return request, nil
}

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
	default:
		base = chat.ErrProviderUnavailable
	}
	return fmt.Errorf("openai: status %d: %s: %w", resp.StatusCode, snippet, base)
}

func (p *Provider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	if err := validateRequest(req); err != nil {
		return chat.Response{}, err
	}
	api := selectWireAPI(req)
	body, warnings, err := api.buildBody(req, false)
	if err != nil {
		return chat.Response{}, err
	}
	request, err := p.newHTTPRequest(ctx, api, body)
	if err != nil {
		return chat.Response{}, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return chat.Response{}, fmt.Errorf("openai: http do: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return chat.Response{}, classifyHTTPError(response)
	}
	return api.decodeResponse(response.Body, warnings)
}

func (p *Provider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	api := selectWireAPI(req)
	body, warnings, err := api.buildBody(req, true)
	if err != nil {
		return nil, err
	}
	request, err := p.newHTTPRequest(ctx, api, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("openai: http do: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		err := classifyHTTPError(response)
		response.Body.Close()
		return nil, err
	}
	return &stream{
		resp: response, reader: bufio.NewReader(response.Body), api: api, pendingWarnings: warnings,
	}, nil
}

type stream struct {
	resp            *http.Response
	reader          *bufio.Reader
	api             wireAPI
	closed          bool
	finished        bool
	doneEmitted     bool
	pendingUsage    *chat.Usage
	pendingWarnings []chat.Warning
	bufferedFinal   *chat.Chunk
}

func (s *stream) Next(ctx context.Context) (chat.Chunk, error) {
	if err := ctx.Err(); err != nil {
		return chat.Chunk{}, err
	}
	if s.closed || s.finished {
		return chat.Chunk{}, io.EOF
	}
	for {
		line, err := s.readLine(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.finished = true
				return s.finalChunk()
			}
			return chat.Chunk{}, err
		}
		if len(line) == 0 || line[0] == ':' || !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[len("data:"):])
		if len(data) == 0 {
			continue
		}
		if bytes.Equal(data, []byte("[DONE]")) {
			s.finished = true
			return s.finalChunk()
		}
		chunk, ok, err := s.api.parseStreamEvent(data)
		if err != nil {
			return chat.Chunk{}, err
		}
		if !ok {
			continue
		}
		if chunk.Usage != nil {
			s.pendingUsage = chunk.Usage
		}
		if _, isChatCompletions := s.api.(chatCompletionsAPI); isChatCompletions && chunk.Done {
			s.bufferedFinal = &chat.Chunk{
				Done: true, Role: chunk.Role, FinishReason: chunk.FinishReason, Usage: chunk.Usage,
			}
			if chunk.Delta != "" || chunk.ReasoningDelta != "" || len(chunk.ToolCallDeltas) > 0 {
				chunk.Done = false
				chunk.FinishReason = ""
				chunk.Usage = nil
				s.attachWarnings(&chunk)
				return chunk, nil
			}
			continue
		}
		if s.bufferedFinal != nil && chunk.Usage != nil && chunk.Delta == "" && chunk.ReasoningDelta == "" && len(chunk.ToolCallDeltas) == 0 {
			continue
		}
		if chunk.Done {
			s.finished = true
			if chunk.Usage == nil {
				chunk.Usage = s.pendingUsage
			}
			s.doneEmitted = true
		}
		s.attachWarnings(&chunk)
		return chunk, nil
	}
}

func (s *stream) readLine(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	line, err := s.reader.ReadBytes('\n')
	line = bytes.TrimRight(line, "\r\n")
	if len(line) > 0 {
		return line, nil
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("openai: stream read: %w", err)
	}
	return nil, nil
}

func (s *stream) finalChunk() (chat.Chunk, error) {
	if s.bufferedFinal != nil {
		chunk := *s.bufferedFinal
		s.bufferedFinal = nil
		if chunk.Usage == nil {
			chunk.Usage = s.pendingUsage
		}
		s.doneEmitted = true
		s.attachWarnings(&chunk)
		return chunk, nil
	}
	if !s.doneEmitted {
		s.doneEmitted = true
		chunk := chat.Chunk{Done: true, Usage: s.pendingUsage}
		s.attachWarnings(&chunk)
		return chunk, nil
	}
	return chat.Chunk{}, io.EOF
}

func (s *stream) attachWarnings(chunk *chat.Chunk) {
	if len(s.pendingWarnings) == 0 {
		return
	}
	chunk.Warnings = s.pendingWarnings
	s.pendingWarnings = nil
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

func imagePartToURL(part chat.ImagePart) (string, error) {
	if strings.TrimSpace(part.URL) != "" {
		return part.URL, nil
	}
	if len(part.Data) == 0 {
		return "", fmt.Errorf("ImagePart has neither URL nor Data")
	}
	mediaType := strings.TrimSpace(part.MediaType)
	if mediaType == "" {
		return "", fmt.Errorf("ImagePart Data requires MediaType")
	}
	return fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(part.Data)), nil
}
