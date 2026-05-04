package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

// RetryChat returns a ChatMiddleware that retries failed Chat and ChatStream
// calls according to cfg, using backoff for delay and retryable to decide
// whether an error is transient.
//
// ChatStream retries only on stream-creation failure; mid-stream errors are
// NOT retried.
func RetryChat(cfg RetryConfig, backoff BackoffStrategy, retryable RetryableError) ChatMiddleware {
	return func(next chat.Provider) chat.Provider {
		return &retryChatProvider{next: next, cfg: cfg, backoff: backoff, retryable: retryable}
	}
}

type retryChatProvider struct {
	next      chat.Provider
	cfg       RetryConfig
	backoff   BackoffStrategy
	retryable RetryableError
}

func (p *retryChatProvider) Name() string { return p.next.Name() }

func (p *retryChatProvider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	var resp chat.Response
	var err error
	for attempt := 0; attempt < p.cfg.MaxAttempts; attempt++ {
		resp, err = p.next.Chat(ctx, req)
		if err == nil || !p.retryable(err) {
			return resp, err
		}
		if attempt < p.cfg.MaxAttempts-1 {
			if waitErr := sleepContext(ctx, p.backoff.Backoff(attempt)); waitErr != nil {
				return resp, waitErr
			}
		}
	}
	return resp, err
}

func (p *retryChatProvider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	for attempt := 0; attempt < p.cfg.MaxAttempts; attempt++ {
		stream, err := p.next.ChatStream(ctx, req)
		if err == nil || !p.retryable(err) {
			return stream, err
		}
		if attempt < p.cfg.MaxAttempts-1 {
			if waitErr := sleepContext(ctx, p.backoff.Backoff(attempt)); waitErr != nil {
				return nil, waitErr
			}
		}
	}
	return p.next.ChatStream(ctx, req)
}
