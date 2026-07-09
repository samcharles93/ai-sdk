package transcribe

import "context"

// Provider is implemented by speech-to-text model backends. Implementations
// translate between the provider-agnostic types defined in this package and
// their underlying API.
type Provider interface {
	// Name returns a short, stable identifier for the provider
	// (for example, "openai", "deepgram").
	Name() string

	// Transcribe converts audio to text.
	Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResponse, error)
}
