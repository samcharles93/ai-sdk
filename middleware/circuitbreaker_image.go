package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/image"
)

// CircuitBreakerImage returns an ImageMiddleware that wraps the provider with
// a circuit breaker. Each invocation of the returned middleware creates an
// independent circuitBreaker instance.
func CircuitBreakerImage(cfg CircuitBreakerConfig) ImageMiddleware {
	return func(next image.Provider) image.Provider {
		cb := &circuitBreaker{cfg: cfg, state: CircuitClosed}
		return &circuitBreakerImageProvider{next: next, cb: cb}
	}
}

type circuitBreakerImageProvider struct {
	next image.Provider
	cb   *circuitBreaker
}

func (w *circuitBreakerImageProvider) Name() string { return w.next.Name() }

func (w *circuitBreakerImageProvider) GenerateImage(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
	if err := w.cb.beforeRequest(); err != nil {
		return image.GenerateImageResponse{}, err
	}
	resp, err := w.next.GenerateImage(ctx, req)
	w.cb.recordResult(err == nil)
	return resp, err
}
