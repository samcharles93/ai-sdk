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
