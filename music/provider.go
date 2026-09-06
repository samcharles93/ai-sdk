package music

import "context"

// Provider is implemented by music generation model backends. Implementations
// translate between the provider-agnostic types defined in this package and
// their underlying API.
type Provider interface {
	// Name returns a short, stable identifier for the provider
	// (for example, "minimax", "suno").
	Name() string

	// GenerateMusic generates a music track from the given request.
	GenerateMusic(ctx context.Context, req GenerateMusicRequest) (GenerateMusicResponse, error)
}
