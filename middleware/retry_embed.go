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
	maxAttempts := p.cfg.attempts()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err = p.next.Embed(ctx, req)
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
