package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/rerank"
)

func RetryRerank(cfg RetryConfig, backoff BackoffStrategy, retryable RetryableError) RerankMiddleware {
	return func(next rerank.Provider) rerank.Provider {
		return &retryRerankProvider{next: next, cfg: cfg, backoff: backoff, retryable: retryable}
	}
}

type retryRerankProvider struct {
	next      rerank.Provider
	cfg       RetryConfig
	backoff   BackoffStrategy
	retryable RetryableError
}

func (p *retryRerankProvider) Name() string { return p.next.Name() }

func (p *retryRerankProvider) Rerank(ctx context.Context, req rerank.Request) (rerank.Response, error) {
	var resp rerank.Response
	var err error
	maxAttempts := p.cfg.attempts()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err = p.next.Rerank(ctx, req)
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
