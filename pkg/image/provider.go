package image

import "context"

// Provider is implemented by image generation model backends. Implementations
// translate between the provider-agnostic types defined in this package and
// their underlying API.
type Provider interface {
	// Name returns a short, stable identifier for the provider
	// (for example, "openai", "fal", "stability").
	Name() string

	// GenerateImage creates one or more images from the given prompt.
	GenerateImage(ctx context.Context, req GenerateImageRequest) (GenerateImageResponse, error)
}
