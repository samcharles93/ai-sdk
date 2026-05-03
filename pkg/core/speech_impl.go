package core

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/pkg/speech"
)

// GenerateSpeech performs a non-streaming speech generation by delegating
// to the provided speech.Provider. It follows the same orchestration
// patterns as GenerateText: respect context cancellation, validate the
// provider, and wrap provider errors with core context.
func GenerateSpeech(ctx context.Context, provider speech.Provider, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
	if provider == nil {
		return speech.GenerateSpeechResponse{}, ErrNoProvider
	}

	if err := ctx.Err(); err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("%w: %v", ErrAborted, err)
	}

	resp, err := provider.GenerateSpeech(ctx, req)
	if err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("core: generate speech: %w", err)
	}
	return resp, nil
}
