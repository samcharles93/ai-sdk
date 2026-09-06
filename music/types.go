package music

// GenerateMusicRequest is a provider-agnostic music generation request.
type GenerateMusicRequest struct {
	// Model identifies the music generation model to use.
	Model string `json:"model"`
	// Prompt describes the music — style, mood, and scenario (for example
	// "Pop, melancholic, perfect for a rainy night").
	Prompt string `json:"prompt"`
	// Lyrics is optional song lyrics, using "\n" to separate lines. When
	// empty and LyricsOptimizer is set, the provider may generate the
	// lyrics from Prompt.
	Lyrics string `json:"lyrics,omitempty"`
	// Instrumental requests instrumental music (no vocals). When true,
	// Lyrics is not required.
	Instrumental bool `json:"instrumental,omitempty"`
	// LyricsOptimizer requests the provider to write lyrics from Prompt
	// when Lyrics is empty.
	LyricsOptimizer bool `json:"lyrics_optimizer,omitempty"`
	// Format is the output audio format (e.g. "mp3", "wav", "pcm").
	Format string `json:"format,omitempty"`
	// ProviderOptions carries provider-specific options.
	ProviderOptions map[string]any `json:"provider_options,omitempty"`
}

// GenerateMusicResponse is the result of a music generation request.
type GenerateMusicResponse struct {
	// Audio contains the raw audio bytes.
	Audio []byte `json:"audio"`
	// Format is the audio format (e.g. "mp3", "wav").
	Format string `json:"format,omitempty"`
}
