package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/image"
)

// CircuitBreakerImageEdit returns an ImageEditMiddleware that wraps the editor
// with a circuit breaker. Each invocation of the returned middleware creates
// an independent circuitBreaker instance.
func CircuitBreakerImageEdit(cfg CircuitBreakerConfig) ImageEditMiddleware {
	return func(next image.Editor) image.Editor {
		cb := &circuitBreaker{cfg: cfg, state: CircuitClosed}
		return &circuitBreakerImageEditProvider{next: next, cb: cb}
	}
}

type circuitBreakerImageEditProvider struct {
	next image.Editor
	cb   *circuitBreaker
}

func (w *circuitBreakerImageEditProvider) Name() string { return w.next.Name() }

func (w *circuitBreakerImageEditProvider) EditImage(ctx context.Context, req image.EditImageRequest) (image.EditImageResponse, error) {
	if err := w.cb.beforeRequest(); err != nil {
		return image.EditImageResponse{}, err
	}
	resp, err := w.next.EditImage(ctx, req)
	w.cb.recordResult(err == nil)
	return resp, err
}
