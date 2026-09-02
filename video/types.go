package video

// VideoMode identifies the video-generation mode. Values are the wire
// strings understood by providers such as xAI.
type VideoMode string

const (
	// VideoModeTextToVideo generates a new video from a text prompt.
	VideoModeTextToVideo VideoMode = "text-to-video"
	// VideoModeEditVideo edits an existing source video.
	VideoModeEditVideo VideoMode = "edit-video"
	// VideoModeExtendVideo extends an existing source video.
	VideoModeExtendVideo VideoMode = "extend-video"
	// VideoModeReferenceToVideo generates a video from reference images.
	VideoModeReferenceToVideo VideoMode = "reference-to-video"
)

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
	// Mode selects the video generation mode (text-to-video, edit-video,
	// extend-video, reference-to-video).
	Mode VideoMode `json:"mode,omitempty"`
	// SourceVideo is the URL of a source video for edit/extend modes.
	SourceVideo string `json:"source_video,omitempty"`
	// ReferenceImages are reference image URLs used by reference-to-video mode.
	ReferenceImages []string `json:"reference_images,omitempty"`
	// Ratio is the requested aspect ratio of the generated video.
	Ratio string `json:"ratio,omitempty"`
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
