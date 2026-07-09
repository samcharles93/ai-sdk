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
	for attempt := 0; attempt < p.cfg.MaxAttempts; attempt++ {
		resp, err = p.next.GenerateImage(ctx, req)
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
