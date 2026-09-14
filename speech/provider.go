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

// VoiceLister is implemented by providers that can enumerate their available
// voices. It is an OPTIONAL capability: callers type-assert a Provider to
// VoiceLister, or use Client.ListVoices, which returns
// ErrVoiceListingNotSupported for providers that do not implement it.
type VoiceLister interface {
	// Name returns the provider identifier, matching Provider.Name.
	Name() string

	// ListVoices returns the voices the provider offers for model. The model
	// scopes the result when voices are model-specific and may be ignored by
	// backends with a global voice set.
	ListVoices(ctx context.Context, model string) ([]Voice, error)
}

// Streamer is implemented by providers that can stream audio as it is
// synthesised. It is an OPTIONAL capability: callers type-assert a Provider to
// Streamer, or use Client.StreamSpeech, which returns ErrStreamNotSupported
// for providers that do not implement it. A provider either supports
// streaming or does not; there is no partial mode.
type Streamer interface {
	// Name returns the provider identifier, matching Provider.Name.
	Name() string

	// StreamSpeech starts a streaming synthesis. It returns an error before
	// any audio is produced when the request cannot be streamed; failures
	// after the first chunk surface from SpeechStream.Next instead. The
	// caller must Close the returned stream when finished.
	StreamSpeech(ctx context.Context, req GenerateSpeechRequest) (SpeechStream, error)
}
