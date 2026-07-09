package middleware

import (
	"context"
	"errors"

	"github.com/samcharles93/ai-sdk/chat"
)

// HealthChecker is implemented by providers that support health probes.
type HealthChecker interface {
	HealthCheck(context.Context) error
}

// ErrHealthCheckFailed is returned when a provider fails its health check.
var ErrHealthCheckFailed = errors.New("health check failed")

// HealthCheckChat wraps a chat.Provider with a pre-call health probe.
// If the provider implements HealthChecker, HealthCheck is called before
// each Chat/ChatStream call. If the health check fails, the call is
// short-circuited with ErrHealthCheckFailed.
func HealthCheckChat() ChatMiddleware {
	return func(next chat.Provider) chat.Provider {
		return &healthCheckChatProvider{next: next}
	}
}

type healthCheckChatProvider struct {
	next chat.Provider
}

func (w *healthCheckChatProvider) Name() string { return w.next.Name() }

func (w *healthCheckChatProvider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	if hc, ok := w.next.(HealthChecker); ok {
		if err := hc.HealthCheck(ctx); err != nil {
			return chat.Response{}, ErrHealthCheckFailed
		}
	}
	return w.next.Chat(ctx, req)
}

func (w *healthCheckChatProvider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	if hc, ok := w.next.(HealthChecker); ok {
		if err := hc.HealthCheck(ctx); err != nil {
			return nil, ErrHealthCheckFailed
		}
	}
	return w.next.ChatStream(ctx, req)
}
