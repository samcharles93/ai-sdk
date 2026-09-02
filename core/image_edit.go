package core

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/image"
)

// GenerateImageEdit orchestrates a non-streaming image edit call. It follows
// the same high-level patterns as GenerateImage: validate the provider,
// respect context cancellation, call through to the provider, and wrap
// sentinel errors with core context.
//
// The provider argument is an [image.Editor] — the optional capability that
// image backends implement to support editing. If it is nil,
// [ErrNoProvider] is returned.
func GenerateImageEdit(ctx context.Context, editor image.Editor, req image.EditImageRequest) (image.EditImageResponse, error) {
	if editor == nil {
		return image.EditImageResponse{}, ErrNoProvider
	}

	if err := ctx.Err(); err != nil {
		return image.EditImageResponse{}, fmt.Errorf("%w: %w", ErrAborted, err)
	}

	resp, err := editor.EditImage(ctx, req)
	if err != nil {
		return image.EditImageResponse{}, fmt.Errorf("core: generate image edit: %w", err)
	}

	return resp, nil
}
