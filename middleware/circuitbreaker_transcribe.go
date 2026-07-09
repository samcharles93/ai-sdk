package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/transcribe"
)

// CircuitBreakerTranscribe returns a TranscribeMiddleware that wraps the
// provider with a circuit breaker. Each invocation of the returned middleware
// creates an independent circuitBreaker instance.
func CircuitBreakerTranscribe(cfg CircuitBreakerConfig) TranscribeMiddleware {
	return func(next transcribe.Provider) transcribe.Provider {
		cb := &circuitBreaker{cfg: cfg, state: CircuitClosed}
		return &circuitBreakerTranscribeProvider{next: next, cb: cb}
	}
}

type circuitBreakerTranscribeProvider struct {
	next transcribe.Provider
	cb   *circuitBreaker
}

func (w *circuitBreakerTranscribeProvider) Name() string { return w.next.Name() }

func (w *circuitBreakerTranscribeProvider) Transcribe(ctx context.Context, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error) {
	if err := w.cb.beforeRequest(); err != nil {
		return transcribe.TranscribeResponse{}, err
	}
	resp, err := w.next.Transcribe(ctx, req)
	w.cb.recordResult(err == nil)
	return resp, err
}
