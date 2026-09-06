// Package openai provides access to OpenAI's Whisper transcription API
// via the transcribe.Provider interface.
package openai

import (
	"context"

	"github.com/samcharles93/ai-sdk/internal/whisper"
	"github.com/samcharles93/ai-sdk/transcribe"
)

// Transcribe converts audio to text using OpenAI's Whisper API.
// It satisfies transcribe.Provider.
func (p *Provider) Transcribe(ctx context.Context, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error) {
	return whisper.Transcribe(ctx, whisper.Config{
		Provider:      "openai",
		BaseURL:       p.baseURL,
		APIKey:        p.apiKey,
		HTTPClient:    p.client,
		IncludeFields: true,
	}, req)
}

// Compile-time assertion that *Provider satisfies transcribe.Provider.
var _ transcribe.Provider = (*Provider)(nil)
