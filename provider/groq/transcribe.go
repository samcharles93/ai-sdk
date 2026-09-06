// Package groq provides access to Groq's Whisper transcription API
// via the transcribe.Provider interface.
package groq

import (
	"context"
	"net/http"

	errx "github.com/samcharles93/ai-sdk/error"
	"github.com/samcharles93/ai-sdk/internal/whisper"
	"github.com/samcharles93/ai-sdk/transcribe"
)

// Transcribe converts audio to text using Groq's Whisper API.
// It satisfies transcribe.Provider.
func (p *Provider) Transcribe(ctx context.Context, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error) {
	return whisper.Transcribe(ctx, whisper.Config{
		Provider:      "groq",
		BaseURL:       p.baseURL,
		APIKey:        p.apiKey,
		HTTPClient:    p.client,
		IncludeFields: false,
		ClassifyError: classifyTranscriptionHTTPError,
	}, req)
}

// classifyTranscriptionHTTPError builds the typed ProviderError Groq returns
// for a non-2xx response. The status→sentinel mapping is shared via
// whisper.ErrorForStatus; only the retryable flag and the typed wrapper differ
// from the default (plain) classification.
func classifyTranscriptionHTTPError(resp *http.Response, snippet string) error {
	base := whisper.ErrorForStatus(resp.StatusCode)
	retryable := resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden
	return errx.NewProviderError("groq", resp, base, snippet, retryable)
}

// Compile-time assertion that *Provider satisfies transcribe.Provider.
var _ transcribe.Provider = (*Provider)(nil)
