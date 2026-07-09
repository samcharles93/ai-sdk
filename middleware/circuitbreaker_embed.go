package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/embed"
)

// CircuitBreakerEmbed returns an EmbedMiddleware that wraps the provider with
// a circuit breaker. Each invocation of the returned middleware creates an
// independent circuitBreaker instance.
func CircuitBreakerEmbed(cfg CircuitBreakerConfig) EmbedMiddleware {
	return func(next embed.Provider) embed.Provider {
		cb := &circuitBreaker{cfg: cfg, state: CircuitClosed}
		return &circuitBreakerEmbedProvider{next: next, cb: cb}
	}
}

type circuitBreakerEmbedProvider struct {
	next embed.Provider
	cb   *circuitBreaker
}

func (w *circuitBreakerEmbedProvider) Name() string { return w.next.Name() }

func (w *circuitBreakerEmbedProvider) Embed(ctx context.Context, req embed.Request) (embed.Response, error) {
	if err := w.cb.beforeRequest(); err != nil {
		return embed.Response{}, err
	}
	resp, err := w.next.Embed(ctx, req)
	w.cb.recordResult(err == nil)
	return resp, err
}
