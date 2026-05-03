package video

import "context"

// Provider is implemented by video generation model backends. Implementations
// translate between the provider-agnostic types defined in this package and
// their underlying API.
type Provider interface {
	// Name returns a short, stable identifier for the provider
	// (for example, "openai", "fal", "stability").
	Name() string

	// GenerateVideo creates one or more videos from the given prompt.
	GenerateVideo(ctx context.Context, req GenerateVideoRequest) (GenerateVideoResponse, error)
}
