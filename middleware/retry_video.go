package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/video"
)

func RetryVideo(cfg RetryConfig, backoff BackoffStrategy, retryable RetryableError) VideoMiddleware {
	return func(next video.Provider) video.Provider {
		return &retryVideoProvider{next: next, cfg: cfg, backoff: backoff, retryable: retryable}
	}
}

type retryVideoProvider struct {
	next      video.Provider
	cfg       RetryConfig
	backoff   BackoffStrategy
	retryable RetryableError
}

func (p *retryVideoProvider) Name() string { return p.next.Name() }

func (p *retryVideoProvider) GenerateVideo(ctx context.Context, req video.GenerateVideoRequest) (video.GenerateVideoResponse, error) {
	var resp video.GenerateVideoResponse
	var err error
	maxAttempts := p.cfg.attempts()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err = p.next.GenerateVideo(ctx, req)
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
