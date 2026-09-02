package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/image"
)

// RetryImageEdit returns an ImageEditMiddleware that retries image-edit calls
// on retryable failures, mirroring RetryImage.
func RetryImageEdit(cfg RetryConfig, backoff BackoffStrategy, retryable RetryableError) ImageEditMiddleware {
	return func(next image.Editor) image.Editor {
		return &retryImageEditProvider{next: next, cfg: cfg, backoff: backoff, retryable: retryable}
	}
}

type retryImageEditProvider struct {
	next      image.Editor
	cfg       RetryConfig
	backoff   BackoffStrategy
	retryable RetryableError
}

func (p *retryImageEditProvider) Name() string { return p.next.Name() }

func (p *retryImageEditProvider) EditImage(ctx context.Context, req image.EditImageRequest) (image.EditImageResponse, error) {
	var resp image.EditImageResponse
	var err error
	maxAttempts := p.cfg.attempts()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err = p.next.EditImage(ctx, req)
		if err == nil || !p.retryable(err) {
			return resp, err
		}
		delay := effectiveDelay(p.backoff, attempt, err)
		if p.cfg.OnAttempt != nil {
			p.cfg.OnAttempt(attempt, err, delay)
		}
		if attempt < maxAttempts-1 {
			if waitErr := sleepContext(ctx, delay); waitErr != nil {
				return resp, waitErr
			}
		}
	}
	return resp, err
}
