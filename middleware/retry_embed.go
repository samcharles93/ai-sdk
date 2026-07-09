package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/embed"
)

func RetryEmbed(cfg RetryConfig, backoff BackoffStrategy, retryable RetryableError) EmbedMiddleware {
	return func(next embed.Provider) embed.Provider {
		return &retryEmbedProvider{next: next, cfg: cfg, backoff: backoff, retryable: retryable}
	}
}

type retryEmbedProvider struct {
	next      embed.Provider
	cfg       RetryConfig
	backoff   BackoffStrategy
	retryable RetryableError
}

func (p *retryEmbedProvider) Name() string { return p.next.Name() }

func (p *retryEmbedProvider) Embed(ctx context.Context, req embed.Request) (embed.Response, error) {
	var resp embed.Response
	var err error
	for attempt := 0; attempt < p.cfg.MaxAttempts; attempt++ {
		resp, err = p.next.Embed(ctx, req)
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
