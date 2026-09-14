package speech

// GenerateSpeechRequest is a provider-agnostic text-to-speech request.
type GenerateSpeechRequest struct {
	// Model identifies the speech model to use.
	Model string `json:"model"`
	// Text is the text to convert to speech.
	Text string `json:"text"`
	// Voice is the voice identifier (e.g. "alloy", "nova").
	Voice string `json:"voice,omitempty"`
	// Instructions is optional voice/style guidance for models that support
	// it (for example OpenAI gpt-4o-mini-tts). Providers that do not support
	// the field reject a non-empty value with ErrInvalidRequest.
	Instructions string `json:"instructions,omitempty"`
	// Speed is the speaking rate multiplier (e.g. 1.0 is normal).
	Speed float64 `json:"speed,omitempty"`
	// SampleRate is the desired output sample rate in Hz for models that
	// support it (for example Groq). Zero leaves the provider default.
	SampleRate int `json:"sample_rate,omitempty"`
	// Format is the output audio format (e.g. "mp3", "wav").
	Format string `json:"format,omitempty"`
	// ProviderOptions carries provider-specific options.
	ProviderOptions map[string]any `json:"provider_options,omitempty"`
}

// GenerateSpeechResponse is the result of a speech generation request.
type GenerateSpeechResponse struct {
	// Audio contains the raw audio data.
	Audio []byte `json:"audio"`
	// Format is the audio format (e.g. "mp3", "wav").
	Format string `json:"format,omitempty"`
}

// Voice describes one voice a speech backend offers. Fields other than ID may
// be empty when the backend does not publish them.
type Voice struct {
	// ID is the value to send in GenerateSpeechRequest.Voice.
	ID string `json:"id"`
	// Name is a human-readable display name; it defaults to ID when the
	// backend does not distinguish the two.
	Name string `json:"name,omitempty"`
	// Language is the voice language (for example "en-us", "ja").
	Language string `json:"language,omitempty"`
	// Gender is the voice gender when the backend publishes it.
	Gender string `json:"gender,omitempty"`
	// Model scopes the voice when voices are model-specific.
	Model string `json:"model,omitempty"`
}

// Usage reports token accounting for a completed streaming synthesis, when
// the provider publishes it.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// SpeechChunk is one piece of a streaming speech synthesis.
type SpeechChunk struct {
	// Data is the decoded audio bytes for this chunk; empty on the Done chunk.
	Data []byte `json:"data,omitempty"`
	// Format is the audio format Data belongs to (e.g. "mp3", "pcm").
	Format string `json:"format,omitempty"`
	// Done marks the final chunk, which carries no Data.
	Done bool `json:"done"`
	// Usage is populated on the Done chunk when the provider reports it.
	Usage *Usage `json:"usage,omitempty"`
}
