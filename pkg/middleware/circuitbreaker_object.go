package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/pkg/object"
)

// CircuitBreakerObject returns an ObjectMiddleware that wraps the provider
// with a circuit breaker. Each invocation of the returned middleware creates
// an independent circuitBreaker instance.
func CircuitBreakerObject(cfg CircuitBreakerConfig) ObjectMiddleware {
	return func(next object.Provider) object.Provider {
		cb := &circuitBreaker{cfg: cfg, state: CircuitClosed}
		return &circuitBreakerObjectProvider{next: next, cb: cb}
	}
}

type circuitBreakerObjectProvider struct {
	next object.Provider
	cb   *circuitBreaker
}

func (w *circuitBreakerObjectProvider) Name() string { return w.next.Name() }

func (w *circuitBreakerObjectProvider) GenerateObject(ctx context.Context, req object.Request) (object.ObjectResult, error) {
	if err := w.cb.beforeRequest(); err != nil {
		return nil, err
	}
	resp, err := w.next.GenerateObject(ctx, req)
	w.cb.recordResult(err == nil)
	return resp, err
}

func (w *circuitBreakerObjectProvider) StreamObject(ctx context.Context, req object.Request) (object.ObjectStream, error) {
	if err := w.cb.beforeRequest(); err != nil {
		return nil, err
	}
	stream, err := w.next.StreamObject(ctx, req)
	w.cb.recordResult(err == nil)
	return stream, err
}
