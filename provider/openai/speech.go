// Package openai provides access to OpenAI's audio generation (TTS) API
// via the speech.Provider interface.
package openai

import (
	"context"

	"github.com/samcharles93/ai-sdk/internal/tts"
	"github.com/samcharles93/ai-sdk/speech"
)

// Model IDs supported by OpenAI's TTS API.
const (
	SpeechModelTTS1        = "tts-1"
	SpeechModelTTS1HD      = "tts-1-hd"
	SpeechModelTTS11106    = "tts-1-1106"
	SpeechModelTTS1HD1106  = "tts-1-hd-1106"
	SpeechModelGPT4MiniTTS = "gpt-4o-mini-tts"
)

const (
	defaultSpeechVoice  = "alloy"
	defaultSpeechFormat = "mp3"
)

// Valid output formats for the speech API.
var validSpeechFormats = map[string]bool{
	"mp3":  true,
	"opus": true,
	"aac":  true,
	"flac": true,
	"wav":  true,
	"pcm":  true,
}

// GenerateSpeech generates speech audio from the given text using OpenAI's
// TTS API. It satisfies speech.Provider.
func (p *Provider) GenerateSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
	return tts.Generate(ctx, tts.Config{
		Provider:       "openai",
		BaseURL:        p.baseURL,
		APIKey:         p.apiKey,
		HTTPClient:     p.client,
		AllowedFormats: validSpeechFormats,
		DefaultVoice:   defaultSpeechVoice,
		DefaultFormat:  defaultSpeechFormat,
		ApplyOptions:   applySpeechOptions,
	}, req)
}

// applySpeechOptions merges OpenAI-specific provider options into the request
// body: an instructions string and a speed override used only when the
// request itself did not set a speed.
func applySpeechOptions(body map[string]any, req speech.GenerateSpeechRequest) {
	opts, ok := req.ProviderOptions["openai"].(map[string]any)
	if !ok {
		return
	}
	if v, ok := opts["instructions"].(string); ok && v != "" {
		body["instructions"] = v
	}
	if v, ok := opts["speed"].(float64); ok && v != 0 && req.Speed == 0 {
		body["speed"] = v
	}
}

// Compile-time assertion that *Provider satisfies speech.Provider.
var _ speech.Provider = (*Provider)(nil)
