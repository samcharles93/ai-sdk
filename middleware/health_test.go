package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

type healthProvider struct {
	healthy bool
	chatFn  func(ctx context.Context, req chat.Request) (chat.Response, error)
}

func (p *healthProvider) Name() string { return "health-provider" }
func (p *healthProvider) HealthCheck(ctx context.Context) error {
	if !p.healthy {
		return errors.New("unhealthy")
	}
	return nil
}

func (p *healthProvider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	return p.chatFn(ctx, req)
}

func (p *healthProvider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	return nil, errors.New("not implemented")
}

func TestHealthCheck_PassesWhenHealthy(t *testing.T) {
	p := &healthProvider{healthy: true, chatFn: func(ctx context.Context, req chat.Request) (chat.Response, error) {
		return chat.Response{Content: "ok"}, nil
	}}
	mw := HealthCheckChat()(p)
	resp, err := mw.Chat(context.Background(), chat.Request{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
}

func TestHealthCheck_FailsWhenUnhealthy(t *testing.T) {
	p := &healthProvider{healthy: false}
	mw := HealthCheckChat()(p)
	_, err := mw.Chat(context.Background(), chat.Request{Model: "m"})
	if !errors.Is(err, ErrHealthCheckFailed) {
		t.Errorf("expected ErrHealthCheckFailed, got %v", err)
	}
}

func TestHealthCheck_SkipsWhenNotImplemented(t *testing.T) {
	// A provider that does NOT implement HealthChecker should work fine
	p := &stubProvider{name: "no-health", chatFn: func(ctx context.Context, req chat.Request) (chat.Response, error) {
		return chat.Response{Content: "ok"}, nil
	}}
	mw := HealthCheckChat()(p)
	resp, err := mw.Chat(context.Background(), chat.Request{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
}
