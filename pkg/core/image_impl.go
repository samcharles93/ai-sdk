package core

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/pkg/image"
)

// GenerateImage orchestrates a non-streaming image generation call.
// It follows the same high-level patterns as GenerateText: validate
// provider, respect context cancellation, call through to the provider,
// and wrap sentinel errors with core context.
func GenerateImage(ctx context.Context, provider image.Provider, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
	if provider == nil {
		return image.GenerateImageResponse{}, ErrNoProvider
	}

	if err := ctx.Err(); err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("%w: %v", ErrAborted, err)
	}

	resp, err := provider.GenerateImage(ctx, req)
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("core: generate image: %w", err)
	}

	return resp, nil
}
