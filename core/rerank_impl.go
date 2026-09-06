package core

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/rerank"
)

// Rerank orchestrates a non-streaming document reranking call. It follows the
// same high-level patterns as GenerateImage: validate the provider, respect
// context cancellation, call through to the provider, and wrap sentinel errors
// with core context.
func Rerank(ctx context.Context, provider rerank.Provider, req rerank.Request) (rerank.Response, error) {
	if provider == nil {
		return rerank.Response{}, ErrNoProvider
	}

	if err := ctx.Err(); err != nil {
		return rerank.Response{}, fmt.Errorf("%w: %w", ErrAborted, err)
	}

	resp, err := provider.Rerank(ctx, req)
	if err != nil {
		return rerank.Response{}, fmt.Errorf("core: rerank: %w", err)
	}

	return resp, nil
}
