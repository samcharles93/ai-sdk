package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/pkg/video"
)

// CircuitBreakerVideo returns a VideoMiddleware that wraps the provider with
// a circuit breaker. Each invocation of the returned middleware creates an
// independent circuitBreaker instance.
func CircuitBreakerVideo(cfg CircuitBreakerConfig) VideoMiddleware {
	return func(next video.Provider) video.Provider {
		cb := &circuitBreaker{cfg: cfg, state: CircuitClosed}
		return &circuitBreakerVideoProvider{next: next, cb: cb}
	}
}

type circuitBreakerVideoProvider struct {
	next video.Provider
	cb   *circuitBreaker
}

func (w *circuitBreakerVideoProvider) Name() string { return w.next.Name() }

func (w *circuitBreakerVideoProvider) GenerateVideo(ctx context.Context, req video.GenerateVideoRequest) (video.GenerateVideoResponse, error) {
	if err := w.cb.beforeRequest(); err != nil {
		return video.GenerateVideoResponse{}, err
	}
	resp, err := w.next.GenerateVideo(ctx, req)
	w.cb.recordResult(err == nil)
	return resp, err
}
