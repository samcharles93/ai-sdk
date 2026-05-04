package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/pkg/speech"
)

func RetrySpeech(cfg RetryConfig, backoff BackoffStrategy, retryable RetryableError) SpeechMiddleware {
	return func(next speech.Provider) speech.Provider {
		return &retrySpeechProvider{next: next, cfg: cfg, backoff: backoff, retryable: retryable}
	}
}

type retrySpeechProvider struct {
	next      speech.Provider
	cfg       RetryConfig
	backoff   BackoffStrategy
	retryable RetryableError
}

func (p *retrySpeechProvider) Name() string { return p.next.Name() }

func (p *retrySpeechProvider) GenerateSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
	var resp speech.GenerateSpeechResponse
	var err error
	for attempt := 0; attempt < p.cfg.MaxAttempts; attempt++ {
		resp, err = p.next.GenerateSpeech(ctx, req)
		if err == nil || !p.retryable(err) {
			return resp, err
		}
		if attempt < p.cfg.MaxAttempts-1 {
			if waitErr := sleepContext(ctx, p.backoff.Backoff(attempt)); waitErr != nil {
				return resp, waitErr
			}
		}
	}
	return resp, err
}
