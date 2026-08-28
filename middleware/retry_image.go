package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/image"
)

func RetryImage(cfg RetryConfig, backoff BackoffStrategy, retryable RetryableError) ImageMiddleware {
	return func(next image.Provider) image.Provider {
		return &retryImageProvider{next: next, cfg: cfg, backoff: backoff, retryable: retryable}
	}
}

type retryImageProvider struct {
	next      image.Provider
	cfg       RetryConfig
	backoff   BackoffStrategy
	retryable RetryableError
}

func (p *retryImageProvider) Name() string { return p.next.Name() }

func (p *retryImageProvider) GenerateImage(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
	var resp image.GenerateImageResponse
	var err error
	maxAttempts := p.cfg.attempts()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err = p.next.GenerateImage(ctx, req)
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
