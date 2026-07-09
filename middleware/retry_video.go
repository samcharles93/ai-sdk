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
	for attempt := 0; attempt < p.cfg.MaxAttempts; attempt++ {
		resp, err = p.next.GenerateVideo(ctx, req)
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
