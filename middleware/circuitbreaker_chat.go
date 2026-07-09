package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/chat"
)

// CircuitBreakerChat returns a ChatMiddleware that wraps the provider with a
// circuit breaker. Each invocation of the returned middleware creates an
// independent circuitBreaker instance so that different provider chains do
// not share failure/success counters.
func CircuitBreakerChat(cfg CircuitBreakerConfig) ChatMiddleware {
	return func(next chat.Provider) chat.Provider {
		cb := &circuitBreaker{cfg: cfg, state: CircuitClosed}
		return &circuitBreakerChatProvider{next: next, cb: cb}
	}
}

type circuitBreakerChatProvider struct {
	next chat.Provider
	cb   *circuitBreaker
}

func (w *circuitBreakerChatProvider) Name() string { return w.next.Name() }

func (w *circuitBreakerChatProvider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	if err := w.cb.beforeRequest(); err != nil {
		return chat.Response{}, err
	}
	resp, err := w.next.Chat(ctx, req)
	w.cb.recordResult(err == nil)
	return resp, err
}

func (w *circuitBreakerChatProvider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	if err := w.cb.beforeRequest(); err != nil {
		return nil, err
	}
	stream, err := w.next.ChatStream(ctx, req)
	w.cb.recordResult(err == nil)
	return stream, err
}
