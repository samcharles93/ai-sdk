package image

// GenerateImageRequest is a provider-agnostic image generation request.
type GenerateImageRequest struct {
	// Model identifies the image generation model to use.
	Model string `json:"model"`
	// Prompt is the text description of the desired image.
	Prompt string `json:"prompt"`
	// NegativePrompt describes what to exclude from the image.
	NegativePrompt string `json:"negative_prompt,omitempty"`
	// N is the number of images to generate. Defaults to 1.
	N int `json:"n,omitempty"`
	// Size is the requested image dimensions (e.g. "1024x1024").
	Size string `json:"size,omitempty"`
	// AspectRatio is the target aspect ratio (e.g. "16:9").
	AspectRatio string `json:"aspect_ratio,omitempty"`
	// Seed enables deterministic generation when supported.
	Seed *int64 `json:"seed,omitempty"`
	// ProviderOptions carries provider-specific options.
	ProviderOptions map[string]any `json:"provider_options,omitempty"`
}

// GeneratedImage represents a single generated image.
type GeneratedImage struct {
	// Data contains the raw image bytes.
	Data []byte `json:"data,omitempty"`
	// URL is a provider-hosted URL for the image.
	URL string `json:"url,omitempty"`
	// Base64 is a base64-encoded representation of the image.
	Base64 string `json:"base64,omitempty"`
	// MediaType is the MIME type (e.g. "image/png").
	MediaType string `json:"media_type,omitempty"`
}

// GenerateImageResponse is the result of an image generation request.
type GenerateImageResponse struct {
	// Images contains the generated images.
	Images []GeneratedImage `json:"images"`
	// Warnings contains non-fatal warnings.
	Warnings []string `json:"warnings,omitempty"`
}
