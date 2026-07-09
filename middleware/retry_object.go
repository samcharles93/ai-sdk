package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/object"
)

func RetryObject(cfg RetryConfig, backoff BackoffStrategy, retryable RetryableError) ObjectMiddleware {
	return func(next object.Provider) object.Provider {
		return &retryObjectProvider{next: next, cfg: cfg, backoff: backoff, retryable: retryable}
	}
}

type retryObjectProvider struct {
	next      object.Provider
	cfg       RetryConfig
	backoff   BackoffStrategy
	retryable RetryableError
}

func (p *retryObjectProvider) Name() string { return p.next.Name() }

func (p *retryObjectProvider) GenerateObject(ctx context.Context, req object.Request) (object.ObjectResult, error) {
	var resp object.ObjectResult
	var err error
	for attempt := 0; attempt < p.cfg.MaxAttempts; attempt++ {
		resp, err = p.next.GenerateObject(ctx, req)
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

func (p *retryObjectProvider) StreamObject(ctx context.Context, req object.Request) (object.ObjectStream, error) {
	for attempt := 0; attempt < p.cfg.MaxAttempts; attempt++ {
		stream, err := p.next.StreamObject(ctx, req)
		if err == nil || !p.retryable(err) {
			return stream, err
		}
		if attempt < p.cfg.MaxAttempts-1 {
			if waitErr := sleepContext(ctx, p.backoff.Backoff(attempt)); waitErr != nil {
				return nil, waitErr
			}
		}
	}
	return p.next.StreamObject(ctx, req)
}
