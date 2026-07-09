package core

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/object"
)

// GenerateObject orchestrates a non-streaming object generation call.
// It follows the same high-level patterns as GenerateImage: validate
// provider, respect context cancellation, call through to the provider,
// and wrap sentinel errors with core context.
func GenerateObject(ctx context.Context, provider object.Provider, req object.Request) (object.ObjectResult, error) {
	if provider == nil {
		return nil, ErrNoProvider
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAborted, err)
	}

	resp, err := provider.GenerateObject(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("core: generate object: %w", err)
	}

	return resp, nil
}

// StreamObject orchestrates a streaming object generation call.
// It validates the provider, respects context cancellation, and
// delegates to the provider. The caller must Close the returned
// ObjectStream when finished.
func StreamObject(ctx context.Context, provider object.Provider, req object.Request) (object.ObjectStream, error) {
	if provider == nil {
		return nil, ErrNoProvider
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAborted, err)
	}

	return provider.StreamObject(ctx, req)
}
