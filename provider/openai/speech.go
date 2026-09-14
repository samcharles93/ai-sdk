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
	voice := defaultSpeechVoice
	format := defaultSpeechFormat
	if p.speech != nil {
		voice = p.speech.DefaultVoice
		if p.speech.DefaultFormat != "" {
			format = p.speech.DefaultFormat
		}
	}
	return tts.Generate(ctx, tts.Config{
		Provider:             "openai",
		BaseURL:              p.baseURL,
		APIKey:               p.apiKey,
		HTTPClient:           p.client,
		AllowedFormats:       validSpeechFormats,
		DefaultVoice:         voice,
		DefaultFormat:        format,
		MaxInputChars:        p.maxInputChars,
		SupportsInstructions: true,
	}, req)
}

// Compile-time assertion that *Provider satisfies speech.Provider.
var _ speech.Provider = (*Provider)(nil)

// openAI voices is the documented OpenAI voice set. It is static: OpenAI has
// no voice-listing endpoint, and model support varies (tts-1/tts-1-hd support
// a smaller set than gpt-4o-mini-tts).
func openAIVoices() []speech.Voice {
	ids := []string{"alloy", "ash", "ballad", "coral", "cedar", "echo", "fable", "marin", "nova", "onyx", "sage", "shimmer", "verse"}
	voices := make([]speech.Voice, 0, len(ids))
	for _, id := range ids {
		voices = append(voices, speech.Voice{ID: id, Name: id})
	}
	return voices
}

// ListVoices returns the voices this provider offers for model. The named
// OpenAI class serves the documented static set; the generic
// OpenAI-compatible class sets Config.DiscoverVoices and queries the server's
// /audio/voices endpoint instead, returning ErrVoiceListingNotSupported when
// the server does not expose one.
func (p *Provider) ListVoices(ctx context.Context, model string) ([]speech.Voice, error) {
	if !p.discoverVoices {
		return openAIVoices(), nil
	}
	return tts.ListVoices(ctx, tts.Config{
		Provider:   "openai",
		BaseURL:    p.baseURL,
		APIKey:     p.apiKey,
		HTTPClient: p.client,
	}, model)
}

// Compile-time assertion that *Provider satisfies speech.VoiceLister.
var _ speech.VoiceLister = (*Provider)(nil)
