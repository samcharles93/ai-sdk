package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/pkg/rerank"
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
	for attempt := 0; attempt < p.cfg.MaxAttempts; attempt++ {
		resp, err = p.next.Rerank(ctx, req)
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
