package transcribe

// TranscribeRequest is a provider-agnostic audio transcription request.
type TranscribeRequest struct {
	// Model identifies the transcription model to use.
	Model string `json:"model"`
	// Audio contains the raw audio data to transcribe.
	Audio []byte `json:"audio"`
	// Language is an optional language hint (e.g. "en", "ja").
	Language string `json:"language,omitempty"`
	// Prompt is an optional guiding text for the transcription style.
	Prompt string `json:"prompt,omitempty"`
	// Temperature controls sampling randomness.
	Temperature float32 `json:"temperature,omitempty"`
	// ProviderOptions carries provider-specific options.
	ProviderOptions map[string]any `json:"provider_options,omitempty"`
}

// TranscriptionSegment is a timed segment of the transcription.
type TranscriptionSegment struct {
	// ID is the segment identifier.
	ID int `json:"id"`
	// Seek is the seek offset in the input audio (in seconds).
	Seek int `json:"seek"`
	// Start is the start time of the segment (in seconds).
	Start float64 `json:"start"`
	// End is the end time of the segment (in seconds).
	End float64 `json:"end"`
	// Text is the transcribed text for this segment.
	Text string `json:"text"`
	// Tokens are the token IDs for this segment, if available.
	Tokens []int `json:"tokens,omitempty"`
	// Temperature is the sampling temperature used for this segment.
	Temperature float64 `json:"temperature,omitempty"`
	// AvgLogprob is the average log probability of the segment.
	AvgLogprob float64 `json:"avg_logprob,omitempty"`
	// CompressionRatio is the compression ratio of the segment.
	CompressionRatio float64 `json:"compression_ratio,omitempty"`
	// NoSpeechProb is the probability that the segment contains no speech.
	NoSpeechProb float64 `json:"no_speech_prob,omitempty"`
}

// TranscribeResponse is the result of a transcription request.
type TranscribeResponse struct {
	// Text is the full transcribed text.
	Text string `json:"text"`
	// Segments contains timed segments of the transcription.
	Segments []TranscriptionSegment `json:"segments,omitempty"`
	// Language is the detected language.
	Language string `json:"language,omitempty"`
	// Duration is the total duration of the audio in seconds.
	Duration float64 `json:"duration,omitempty"`
	// Warnings contains non-fatal warnings.
	Warnings []string `json:"warnings,omitempty"`
}
