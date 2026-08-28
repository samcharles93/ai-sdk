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
	maxAttempts := p.cfg.attempts()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err = p.next.GenerateObject(ctx, req)
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

func (p *retryObjectProvider) StreamObject(ctx context.Context, req object.Request) (object.ObjectStream, error) {
	var stream object.ObjectStream
	var err error
	maxAttempts := p.cfg.attempts()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		stream, err = p.next.StreamObject(ctx, req)
		if err == nil || !p.retryable(err) {
			return stream, err
		}
		delay := effectiveDelay(p.backoff, attempt, err)
		if p.cfg.OnAttempt != nil {
			p.cfg.OnAttempt(attempt, err, delay)
		}
		if attempt < maxAttempts-1 {
			if waitErr := sleepContext(ctx, delay); waitErr != nil {
				return stream, waitErr
			}
		}
	}
	// All attempts exhausted: return the last captured error rather than
	// issuing an additional unscheduled call (which would exceed MaxAttempts).
	return stream, err
}
