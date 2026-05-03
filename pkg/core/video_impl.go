package core

import (
    "context"
    "fmt"

    "github.com/samcharles93/ai-sdk/pkg/video"
)

// GenerateVideo orchestrates a non-streaming video generation call.
// It follows the same high-level patterns as GenerateImage: validate
// provider, respect context cancellation, call through to the provider,
// and wrap sentinel errors with core context.
func GenerateVideo(ctx context.Context, provider video.Provider, req video.GenerateVideoRequest) (video.GenerateVideoResponse, error) {
    if provider == nil {
        return video.GenerateVideoResponse{}, ErrNoProvider
    }

    if err := ctx.Err(); err != nil {
        return video.GenerateVideoResponse{}, fmt.Errorf("%w: %v", ErrAborted, err)
    }

    resp, err := provider.GenerateVideo(ctx, req)
    if err != nil {
        return video.GenerateVideoResponse{}, fmt.Errorf("core: generate video: %w", err)
    }

    return resp, nil
}
