package core

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/speech"
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
		return speech.GenerateSpeechResponse{}, fmt.Errorf("%w: %w", ErrAborted, err)
	}

	resp, err := provider.GenerateSpeech(ctx, req)
	if err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("core: generate speech: %w", err)
	}
	return resp, nil
}

// StreamSpeech starts a streaming speech synthesis, delegating to the
// provider's optional speech.Streamer capability. It returns
// speech.ErrStreamNotSupported when the provider cannot stream. The caller
// must Close the returned stream when finished.
func StreamSpeech(ctx context.Context, provider speech.Provider, req speech.GenerateSpeechRequest) (speech.SpeechStream, error) {
	if provider == nil {
		return nil, ErrNoProvider
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAborted, err)
	}

	streamer, ok := provider.(speech.Streamer)
	if !ok {
		return nil, fmt.Errorf("core: stream speech: %w", speech.ErrStreamNotSupported)
	}

	stream, err := streamer.StreamSpeech(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("core: stream speech: %w", err)
	}
	return stream, nil
}
