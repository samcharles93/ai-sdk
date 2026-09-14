package groq

import (
	"context"
	"net/http"

	errx "github.com/samcharles93/ai-sdk/error"
	"github.com/samcharles93/ai-sdk/internal/tts"
	"github.com/samcharles93/ai-sdk/speech"
)

const defaultSpeechFormat = "mp3"

// groqSpeechFormats is the response_format set Groq's OpenAI-compatible
// speech endpoint accepts.
var groqSpeechFormats = map[string]bool{
	"flac":  true,
	"mp3":   true,
	"mulaw": true,
	"ogg":   true,
	"wav":   true,
}

// GenerateSpeech synthesises speech using Groq's OpenAI-compatible speech
// endpoint. Groq has no cross-model default voice (PlayAI voices are
// model-scoped and Orpheus uses a different naming scheme), so a request that
// omits Voice is rejected rather than guessed. It satisfies speech.Provider.
func (p *Provider) GenerateSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
	return tts.Generate(ctx, tts.Config{
		Provider:       "groq",
		BaseURL:        p.baseURL,
		APIKey:         p.apiKey,
		HTTPClient:     p.client,
		AllowedFormats: groqSpeechFormats,
		DefaultFormat:  defaultSpeechFormat,
		ClassifyError:  classifySpeechHTTPError,
	}, req)
}

// classifySpeechHTTPError builds the typed ProviderError Groq returns for a
// non-2xx speech response. The status mapping mirrors the chat classifier:
// authentication failures and invalid requests are terminal, rate limits and
// server-side failures are retryable. Note this deliberately differs from the
// transcribe classifier, which treats a 400 as retryable; on the speech path a
// 400 means the voice or format is invalid, so retrying cannot succeed.
func classifySpeechHTTPError(resp *http.Response, snippet string) error {
	var base error
	retryable := false
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		base = speech.ErrAuthFailed
	case resp.StatusCode == http.StatusTooManyRequests:
		base = speech.ErrRateLimited
		retryable = true
	case resp.StatusCode == http.StatusBadRequest:
		base = speech.ErrInvalidRequest
	case resp.StatusCode >= 500:
		base = speech.ErrProviderUnavailable
		retryable = true
	default:
		base = speech.ErrProviderUnavailable
		retryable = true
	}
	return errx.NewProviderError("groq", resp, base, snippet, retryable)
}

// Compile-time assertion that *Provider satisfies speech.Provider.
var _ speech.Provider = (*Provider)(nil)
