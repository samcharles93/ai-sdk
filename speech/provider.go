package speech

import "context"

// Provider is implemented by text-to-speech model backends. Implementations
// translate between the provider-agnostic types defined in this package and
// their underlying API.
type Provider interface {
	// Name returns a short, stable identifier for the provider
	// (for example, "openai", "elevenlabs").
	Name() string

	// GenerateSpeech generates speech audio from the given text.
	GenerateSpeech(ctx context.Context, req GenerateSpeechRequest) (GenerateSpeechResponse, error)
}
