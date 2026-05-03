package video

// GenerateVideoRequest is a provider-agnostic video generation request.
type GenerateVideoRequest struct {
	// Model identifies the video generation model to use.
	Model string `json:"model"`
	// Prompt is the text description of the desired video.
	Prompt string `json:"prompt"`
	// Duration suggests the desired length (for example, "00:00:10" or seconds as a string).
	Duration string `json:"duration,omitempty"`
	// Resolution is the requested video resolution (e.g. "1920x1080").
	Resolution string `json:"resolution,omitempty"`
	// FrameRate is the requested frames per second.
	FrameRate int `json:"frame_rate,omitempty"`
	// ProviderOptions carries provider-specific options.
	ProviderOptions map[string]any `json:"provider_options,omitempty"`
}

// VideoResult represents a single generated video asset.
type VideoResult struct {
	// Data contains the raw video bytes.
	Data []byte `json:"data,omitempty"`
	// URL is a provider-hosted URL for the video.
	URL string `json:"url,omitempty"`
	// MediaType is the MIME type (e.g. "video/mp4").
	MediaType string `json:"media_type,omitempty"`
}

// GenerateVideoResponse is the result of a video generation request.
type GenerateVideoResponse struct {
	// Videos contains the generated video assets.
	Videos []VideoResult `json:"videos"`
	// Warnings contains non-fatal warnings.
	Warnings []string `json:"warnings,omitempty"`
}
