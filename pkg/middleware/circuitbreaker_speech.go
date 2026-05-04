package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/pkg/speech"
)

// CircuitBreakerSpeech returns a SpeechMiddleware that wraps the provider with
// a circuit breaker. Each invocation of the returned middleware creates an
// independent circuitBreaker instance.
func CircuitBreakerSpeech(cfg CircuitBreakerConfig) SpeechMiddleware {
	return func(next speech.Provider) speech.Provider {
		cb := &circuitBreaker{cfg: cfg, state: CircuitClosed}
		return &circuitBreakerSpeechProvider{next: next, cb: cb}
	}
}

type circuitBreakerSpeechProvider struct {
	next speech.Provider
	cb   *circuitBreaker
}

func (w *circuitBreakerSpeechProvider) Name() string { return w.next.Name() }

func (w *circuitBreakerSpeechProvider) GenerateSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
	if err := w.cb.beforeRequest(); err != nil {
		return speech.GenerateSpeechResponse{}, err
	}
	resp, err := w.next.GenerateSpeech(ctx, req)
	w.cb.recordResult(err == nil)
	return resp, err
}
