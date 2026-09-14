// Package tts provides a provider-agnostic implementation of the
// OpenAI-compatible /audio/speech endpoint (JSON request, binary audio
// response, non-2xx classification) shared by the provider packages that
// target it (currently openai and groq).
//
// It lives outside the domain layers so those packages stay stdlib-only, and
// is parameterised by the provider-specific bits (allowed output formats,
// default voice/format, supported optional fields, error classification) so
// each provider becomes a thin wrapper rather than a near-copy of the same
// request/response logic.
package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/speech"
)

const endpoint = "/audio/speech"

// Config parameterises the shared OpenAI-compatible speech helper for a
// specific provider backend.
type Config struct {
	// Provider is the provider name. It is used in every error message that
	// would otherwise be provider-specific (for example "openai" or "groq"),
	// and as the ProviderOptions bucket key.
	Provider string
	// BaseURL is the API root; /audio/speech is appended.
	BaseURL string
	// APIKey is the bearer token used in the Authorization header.
	APIKey string
	// HTTPClient is the client used to perform the request.
	HTTPClient *http.Client
	// AllowedFormats is the client-side validation set for the output audio
	// format. A request whose resolved format is absent is rejected with
	// speech.ErrInvalidRequest before any network call.
	AllowedFormats map[string]bool
	// DefaultVoice is used when neither the request nor the provider options
	// name a voice. When it is empty and no voice is resolved, the request is
	// rejected: providers whose voices are model-scoped have no safe default.
	DefaultVoice string
	// DefaultFormat is used when neither the request nor the provider options
	// name a format. It must be a member of AllowedFormats.
	DefaultFormat string
	// SupportsInstructions reports whether the backend accepts the
	// OpenAI-compatible instructions field. When false, a request that
	// resolves a non-empty instructions value is rejected with
	// speech.ErrInvalidRequest instead of silently dropping it.
	SupportsInstructions bool
	// SupportsSampleRate reports whether the backend accepts the
	// OpenAI-compatible sample_rate field, with the same rejection contract
	// as SupportsInstructions.
	SupportsSampleRate bool
	// ClassifyError, when non-nil, builds the error returned for a non-2xx
	// response. It receives the HTTP response (for headers such as
	// Retry-After / X-Request-Id) and the sanitised body snippet. When nil a
	// default classification is used (a plain fmt.Errorf wrapping
	// [ErrorForStatus]).
	ClassifyError func(resp *http.Response, snippet string) error
}

// Generate performs an OpenAI-compatible speech synthesis call. It resolves
// the voice, format, speed, instructions and sample rate from the request and
// its provider options, builds the JSON body, executes it against
// cfg.BaseURL + /audio/speech, classifies non-2xx responses, and returns the
// raw audio.
func Generate(ctx context.Context, cfg Config, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
	if req.Model == "" {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("%s: model is required: %w", cfg.Provider, speech.ErrInvalidRequest)
	}
	if req.Text == "" {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("%s: text is required: %w", cfg.Provider, speech.ErrInvalidRequest)
	}

	opts, err := speech.ProviderOptionsFor[speech.Options](req.ProviderOptions, cfg.Provider)
	if err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("%s: read provider options: %w", cfg.Provider, err)
	}

	voice := req.Voice
	if voice == "" {
		voice = opts.Voice
	}
	if voice == "" {
		voice = cfg.DefaultVoice
	}
	if voice == "" {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("%s: voice is required: %w", cfg.Provider, speech.ErrInvalidRequest)
	}

	format := req.Format
	if format == "" {
		format = opts.Format
	}
	if format == "" {
		format = cfg.DefaultFormat
	}
	if !cfg.AllowedFormats[format] {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("%s: invalid output format %q: %w", cfg.Provider, format, speech.ErrInvalidRequest)
	}

	body := map[string]any{
		"model":           req.Model,
		"input":           req.Text,
		"voice":           voice,
		"response_format": format,
	}
	if speed := resolveSpeed(req, opts); speed != 0 {
		body["speed"] = speed
	}
	if err := applyInstructions(cfg, body, req, opts); err != nil {
		return speech.GenerateSpeechResponse{}, err
	}
	if err := applySampleRate(cfg, body, req, opts); err != nil {
		return speech.GenerateSpeechResponse{}, err
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("%s: marshal speech request: %w", cfg.Provider, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+endpoint, bytes.NewReader(buf))
	if err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("%s: build speech request: %w", cfg.Provider, err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "audio/*")

	resp, err := cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("%s: http do: %w", cfg.Provider, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		snippet := chat.SanitizeErrorBody(respBody)
		if cfg.ClassifyError != nil {
			return speech.GenerateSpeechResponse{}, cfg.ClassifyError(resp, snippet)
		}
		return speech.GenerateSpeechResponse{}, defaultError(cfg.Provider, resp.StatusCode, snippet)
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("%s: read audio response: %w", cfg.Provider, err)
	}

	return speech.GenerateSpeechResponse{
		Audio:  audio,
		Format: format,
	}, nil
}

// resolveSpeed returns the speaking rate, preferring the request field over
// the provider option.
func resolveSpeed(req speech.GenerateSpeechRequest, opts speech.Options) float64 {
	if req.Speed != 0 {
		return req.Speed
	}
	return opts.Speed
}

// applyInstructions writes the resolved instructions into body, rejecting the
// request when the backend does not support the field.
func applyInstructions(cfg Config, body map[string]any, req speech.GenerateSpeechRequest, opts speech.Options) error {
	instructions := req.Instructions
	if instructions == "" {
		instructions = opts.Instructions
	}
	if instructions == "" {
		return nil
	}
	if !cfg.SupportsInstructions {
		return fmt.Errorf("%s: instructions are not supported: %w", cfg.Provider, speech.ErrInvalidRequest)
	}
	body["instructions"] = instructions
	return nil
}

// applySampleRate writes the resolved sample rate into body, rejecting the
// request when the backend does not support the field.
func applySampleRate(cfg Config, body map[string]any, req speech.GenerateSpeechRequest, opts speech.Options) error {
	sampleRate := req.SampleRate
	if sampleRate == 0 {
		sampleRate = opts.SampleRate
	}
	if sampleRate <= 0 {
		return nil
	}
	if !cfg.SupportsSampleRate {
		return fmt.Errorf("%s: sample_rate is not supported: %w", cfg.Provider, speech.ErrInvalidRequest)
	}
	body["sample_rate"] = sampleRate
	return nil
}

// ErrorForStatus maps an OpenAI-compatible speech HTTP status code to the
// corresponding speech sentinel error. It is shared by every provider so the
// classification switch lives in one place rather than being duplicated.
// 400/404/422 are terminal request errors (unknown model, invalid voice or
// format), 429 is retryable rate limiting, 401/403 are authentication
// failures, and everything else is a transient provider failure.
func ErrorForStatus(code int) error {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return speech.ErrAuthFailed
	case http.StatusTooManyRequests:
		return speech.ErrRateLimited
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
		return speech.ErrInvalidRequest
	default:
		return speech.ErrProviderUnavailable
	}
}

// defaultError builds the plain wrapped error used when Config.ClassifyError
// is nil. It reproduces the "<provider>: status %d: <snippet>: <sentinel>"
// shape some providers (openai) return for non-2xx responses.
func defaultError(provider string, code int, snippet string) error {
	return fmt.Errorf("%s: status %d: %s: %w", provider, code, snippet, ErrorForStatus(code))
}
