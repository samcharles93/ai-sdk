package core

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/music"
)

// GenerateMusic orchestrates a non-streaming music generation call. It
// follows the same high-level patterns as GenerateImage: validate the
// provider, respect context cancellation, call through to the provider, and
// wrap sentinel errors with core context.
func GenerateMusic(ctx context.Context, provider music.Provider, req music.GenerateMusicRequest) (music.GenerateMusicResponse, error) {
	if provider == nil {
		return music.GenerateMusicResponse{}, ErrNoProvider
	}

	if err := ctx.Err(); err != nil {
		return music.GenerateMusicResponse{}, fmt.Errorf("%w: %w", ErrAborted, err)
	}

	resp, err := provider.GenerateMusic(ctx, req)
	if err != nil {
		return music.GenerateMusicResponse{}, fmt.Errorf("core: generate music: %w", err)
	}

	return resp, nil
}
