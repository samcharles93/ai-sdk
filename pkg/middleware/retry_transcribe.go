package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/pkg/transcribe"
)

func RetryTranscribe(cfg RetryConfig, backoff BackoffStrategy, retryable RetryableError) TranscribeMiddleware {
	return func(next transcribe.Provider) transcribe.Provider {
		return &retryTranscribeProvider{next: next, cfg: cfg, backoff: backoff, retryable: retryable}
	}
}

type retryTranscribeProvider struct {
	next      transcribe.Provider
	cfg       RetryConfig
	backoff   BackoffStrategy
	retryable RetryableError
}

func (p *retryTranscribeProvider) Name() string { return p.next.Name() }

func (p *retryTranscribeProvider) Transcribe(ctx context.Context, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error) {
	var resp transcribe.TranscribeResponse
	var err error
	for attempt := 0; attempt < p.cfg.MaxAttempts; attempt++ {
		resp, err = p.next.Transcribe(ctx, req)
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
