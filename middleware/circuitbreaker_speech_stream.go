package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/speech"
)

// CircuitBreakerSpeechStream returns a SpeechStreamMiddleware that wraps the
// streamer with a circuit breaker. The breaker guards stream establishment
// only: once StreamSpeech returns a stream successfully, the breaker records
// success rather than waiting for the stream to finish, because a failure
// after the first chunk cannot be retried safely.
func CircuitBreakerSpeechStream(cfg CircuitBreakerConfig) SpeechStreamMiddleware {
	return func(next speech.Streamer) speech.Streamer {
		cb := &circuitBreaker{cfg: cfg, state: CircuitClosed}
		return &circuitBreakerSpeechStream{next: next, cb: cb}
	}
}

type circuitBreakerSpeechStream struct {
	next speech.Streamer
	cb   *circuitBreaker
}

func (w *circuitBreakerSpeechStream) Name() string { return w.next.Name() }

func (w *circuitBreakerSpeechStream) StreamSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.SpeechStream, error) {
	if err := w.cb.beforeRequest(); err != nil {
		return nil, err
	}
	stream, err := w.next.StreamSpeech(ctx, req)
	w.cb.recordResult(err == nil)
	return stream, err
}
