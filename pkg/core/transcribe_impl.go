package core

import (
    "context"
    "fmt"

    "github.com/samcharles93/ai-sdk/pkg/transcribe"
)

// Transcribe orchestrates a non-streaming transcription call.
// It validates the provider, respects context cancellation, delegates
// to the provider, and wraps provider errors with core context.
func Transcribe(ctx context.Context, provider transcribe.Provider, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error) {
    if provider == nil {
        return transcribe.TranscribeResponse{}, ErrNoProvider
    }

    if err := ctx.Err(); err != nil {
        return transcribe.TranscribeResponse{}, fmt.Errorf("%w: %v", ErrAborted, err)
    }

    resp, err := provider.Transcribe(ctx, req)
    if err != nil {
        return transcribe.TranscribeResponse{}, fmt.Errorf("core: transcribe: %w", err)
    }
    return resp, nil
}
