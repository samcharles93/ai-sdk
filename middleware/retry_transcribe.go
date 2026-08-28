package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/transcribe"
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
	maxAttempts := p.cfg.attempts()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err = p.next.Transcribe(ctx, req)
		if err == nil || !p.retryable(err) {
			return resp, err
		}
		delay := effectiveDelay(p.backoff, attempt, err)
		if p.cfg.OnAttempt != nil {
			p.cfg.OnAttempt(attempt, err, delay)
		}
		if attempt < maxAttempts-1 {
			if waitErr := sleepContext(ctx, delay); waitErr != nil {
				return resp, waitErr
			}
		}
	}
	return resp, err
}
