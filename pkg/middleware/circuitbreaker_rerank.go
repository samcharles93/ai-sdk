package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/pkg/rerank"
)

// CircuitBreakerRerank returns a RerankMiddleware that wraps the provider with
// a circuit breaker. Each invocation of the returned middleware creates an
// independent circuitBreaker instance.
func CircuitBreakerRerank(cfg CircuitBreakerConfig) RerankMiddleware {
	return func(next rerank.Provider) rerank.Provider {
		cb := &circuitBreaker{cfg: cfg, state: CircuitClosed}
		return &circuitBreakerRerankProvider{next: next, cb: cb}
	}
}

type circuitBreakerRerankProvider struct {
	next rerank.Provider
	cb   *circuitBreaker
}

func (w *circuitBreakerRerankProvider) Name() string { return w.next.Name() }

func (w *circuitBreakerRerankProvider) Rerank(ctx context.Context, req rerank.Request) (rerank.Response, error) {
	if err := w.cb.beforeRequest(); err != nil {
		return rerank.Response{}, err
	}
	resp, err := w.next.Rerank(ctx, req)
	w.cb.recordResult(err == nil)
	return resp, err
}
