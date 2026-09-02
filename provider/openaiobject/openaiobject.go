package openaiobject

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/samcharles93/ai-sdk/chat"
	errx "github.com/samcharles93/ai-sdk/error"
	"github.com/samcharles93/ai-sdk/object"
)

const (
	defaultBaseURL      = "https://api.openai.com"
	chatCompletionsPath = "/chat/completions"
)

// Config configures the OpenAI object generation provider.
type Config struct {
	// APIKey authenticates requests to OpenAI.
	APIKey string
	// BaseURL overrides the API root. When empty, the OpenAI default is
	// used. A base URL with no path segment gets "/v1" appended, matching
	// OpenAI's actual API root.
	BaseURL string
	// HTTPClient overrides the client used for requests. If nil, requests
	// are bounded only by their caller contexts.
	HTTPClient *http.Client
}

// Provider implements object.Provider over OpenAI's Chat Completions API.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

var _ object.Provider = (*Provider)(nil)

// New constructs an OpenAI object generation provider.
func New(cfg Config) (*Provider, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	if cfg.APIKey == "" && base == defaultBaseURL {
		return nil, fmt.Errorf("openaiobject: APIKey is required for api.openai.com: %w", object.ErrInvalidRequest)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
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
func (p *Provider) Name() string { return "openaiobject" }

func validateRequest(req object.Request) error {
	if req.Model == "" {
		return fmt.Errorf("openaiobject: model is required: %w", object.ErrInvalidRequest)
	}
	return nil
}

// buildBody constructs the Chat Completions request body for object
// generation. When req.Schema is set, response_format.json_schema (strict)
// constrains the output; otherwise it falls back to response_format.json_object.
func buildBody(req object.Request, stream bool) (map[string]any, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	body := map[string]any{
		"model":    req.Model,
		"messages": []map[string]any{{"role": "user", "content": req.Prompt}},
		"stream":   stream,
	}
	if len(req.Schema) > 0 {
		body["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "object",
				"strict": true,
				"schema": req.Schema,
			},
		}
	} else {
		body["response_format"] = map[string]any{"type": "json_object"}
	}
	if req.MaxTokens != 0 {
		body["max_tokens"] = req.MaxTokens
	}
	return body, nil
}

func (p *Provider) newHTTPRequest(ctx context.Context, body map[string]any, stream bool) (*http.Request, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openaiobject: marshal request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+chatCompletionsPath, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("openaiobject: build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("Content-Type", "application/json")
	if stream {
		request.Header.Set("Accept", "text/event-stream")
	} else {
		request.Header.Set("Accept", "application/json")
	}
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
	retryable := false
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		base = object.ErrAuthFailed
	case resp.StatusCode == http.StatusTooManyRequests:
		base = object.ErrRateLimited
		retryable = true
	case resp.StatusCode == http.StatusBadRequest:
		base = object.ErrInvalidRequest
	default:
		base = object.ErrProviderUnavailable
		retryable = true
	}
	return errx.NewProviderError("openaiobject", resp, base, snippet, retryable)
}

type wireResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// GenerateObject performs a non-streaming object generation request. It
// POSTs to {baseURL}/chat/completions and returns the model's JSON payload
// as an object.Object (whose Content holds the raw JSON text).
func (p *Provider) GenerateObject(ctx context.Context, req object.Request) (object.ObjectResult, error) {
	body, err := buildBody(req, false)
	if err != nil {
		return nil, err
	}
	request, err := p.newHTTPRequest(ctx, body, false)
	if err != nil {
		return nil, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("openaiobject: http do: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, classifyHTTPError(response)
	}

	var wire wireResponse
	if err := json.NewDecoder(response.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("openaiobject: decode response: %w", err)
	}
	if len(wire.Choices) == 0 {
		return nil, fmt.Errorf("openaiobject: no choices in response: %w", object.ErrProviderUnavailable)
	}
	content := wire.Choices[0].Message.Content
	if content == "" {
		return nil, fmt.Errorf("openaiobject: empty content in response: %w", object.ErrProviderUnavailable)
	}
	return object.Object{Name: "object", Content: content}, nil
}

type wireStreamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content string `json:"content"`
			Role    string `json:"role"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// StreamObject performs a streaming object generation request. It POSTs to
// {baseURL}/chat/completions with stream:true, parses SSE content deltas,
// and emits object.ObjectChunk values whose Delta fields concatenate into
// the model's JSON. It emits a final Done chunk before returning io.EOF.
func (p *Provider) StreamObject(ctx context.Context, req object.Request) (object.ObjectStream, error) {
	body, err := buildBody(req, true)
	if err != nil {
		return nil, err
	}
	request, err := p.newHTTPRequest(ctx, body, true)
	if err != nil {
		return nil, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("openaiobject: http do: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		err := classifyHTTPError(response)
		response.Body.Close()
		return nil, err
	}
	return &objectStream{
		resp:   response,
		reader: bufio.NewReader(response.Body),
	}, nil
}

type objectStream struct {
	resp     *http.Response
	reader   *bufio.Reader
	closed   bool
	finished bool
	doneSent bool
}

func (s *objectStream) Next(ctx context.Context) (object.ObjectChunk, error) {
	if err := ctx.Err(); err != nil {
		return object.ObjectChunk{}, err
	}
	if s.closed {
		return object.ObjectChunk{}, io.EOF
	}
	for {
		if s.finished {
			if s.doneSent {
				return object.ObjectChunk{}, io.EOF
			}
			s.doneSent = true
			return object.ObjectChunk{Done: true}, nil
		}

		line, err := s.readLine(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.finished = true
				continue
			}
			return object.ObjectChunk{}, err
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
			continue
		}

		var wire wireStreamChunk
		if err := json.Unmarshal(data, &wire); err != nil {
			return object.ObjectChunk{}, fmt.Errorf("openaiobject: decode stream chunk: %w", err)
		}
		if len(wire.Choices) == 0 {
			continue
		}
		if wire.Choices[0].FinishReason != "" {
			s.finished = true
		}
		if content := wire.Choices[0].Delta.Content; content != "" {
			return object.ObjectChunk{Delta: content}, nil
		}
		if wire.Choices[0].FinishReason != "" {
			continue
		}
	}
}

func (s *objectStream) readLine(ctx context.Context) ([]byte, error) {
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
		return nil, fmt.Errorf("openaiobject: stream read: %w", err)
	}
	return nil, nil
}

func (s *objectStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.resp != nil && s.resp.Body != nil {
		return s.resp.Body.Close()
	}
	return nil
}
