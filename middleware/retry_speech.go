package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/speech"
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
	maxAttempts := p.cfg.attempts()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err = p.next.GenerateSpeech(ctx, req)
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
