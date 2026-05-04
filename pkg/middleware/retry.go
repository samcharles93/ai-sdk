package middleware

import (
	"context"
	"math/rand"
	"strings"
	"time"

	errx "github.com/samcharles93/ai-sdk/pkg/error"
)

// BackoffStrategy calculates the delay before a retry attempt.
type BackoffStrategy interface {
	Backoff(attempt int) time.Duration
}

// ExponentialBackoff implements exponential backoff with full jitter.
//
//	Formula: min(base * multiplier^attempt, max) * (1 + jitter*(rand*2-1))
//
// Zero-value fields are safe: a zero Backoff uses BaseDelay=1s, MaxDelay=30s,
// Multiplier=2, Jitter=0.5.
type ExponentialBackoff struct {
	// BaseDelay is the initial delay for the first retry attempt (attempt 0).
	// Defaults to 1s.
	BaseDelay time.Duration

	// MaxDelay caps the exponential backoff. Defaults to 30s.
	MaxDelay time.Duration

	// Multiplier is the exponential growth factor. Defaults to 2.0.
	Multiplier float64

	// Jitter is the full-jitter factor (0.0–1.0). Defaults to 0.5.
	Jitter float64
}

func (b ExponentialBackoff) Backoff(attempt int) time.Duration {
	if attempt <= 0 {
		return b.BaseDelay
	}

	base := float64(b.BaseDelay)
	if base <= 0 {
		base = float64(time.Second)
	}

	max := float64(b.MaxDelay)
	if max <= 0 {
		max = float64(30 * time.Second)
	}

	multiplier := b.Multiplier
	if multiplier <= 0 {
		multiplier = 2.0
	}

	jitter := b.Jitter
	if jitter < 0 {
		jitter = 0.5
	}

	backoff := base
	limit := max
	for i := 0; i < attempt && backoff < limit; i++ {
		backoff *= multiplier
	}
	if backoff > limit {
		backoff = limit
	}
	if jitter > 0 {
		backoff *= 1 + jitter*(rand.Float64()*2-1)
	}
	if backoff < 0 {
		return 0
	}
	return time.Duration(backoff)
}

// RetryConfig controls retry behaviour.
type RetryConfig struct {
	// MaxAttempts is the total number of attempts (1 = no retries).
	MaxAttempts int
}

// RetryableError returns true if the error should trigger a retry.
type RetryableError func(error) bool

// DefaultRetryableError retries on provider-unavailable, rate-limiting, and
// temporary network errors. It does NOT retry auth failures or invalid
// requests (which would always fail on retry).
//
// The function matches against the shared [errx] sentinels and falls back to
// substring matching in the error message for common transient keywords when
// the error cannot be unwrapped to a known sentinel.
func DefaultRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Sentinels that always warrant a retry.
	retryableSentinels := []error{
		errx.ErrTimeout,
		errx.ErrProviderNotAvailable,
		errx.ErrQuotaExceeded,
	}

	for _, sentinel := range retryableSentinels {
		if isOrContains(err, sentinel) {
			// ErrQuotaExceeded may include auth/quota issues — check for auth
			// exclusion *after* the sentinel match lets us short-circuit the
			// common retryable case.
			if sentinel == errx.ErrQuotaExceeded && isQuotaPermanent(err) {
				return false
			}
			return true
		}
	}

	// Explicit auth / invalid-request exclusions.
	nonRetryable := []string{
		"auth", "unauthorized", "forbidden", "401", "403",
		"invalid request", "invalid api key",
	}
	errMsg := strings.ToLower(err.Error())
	for _, keyword := range nonRetryable {
		if strings.Contains(errMsg, keyword) {
			return false
		}
	}

	// Fallback: transient keyword detection.
	retryableKeywords := []string{
		"rate limit", "too many requests", "429",
		"unavailable", "temporarily",
		"network", "connection refused", "connection reset",
		"timeout", "timed out",
		"server error", "internal server error",
		"service unavailable", "503",
		"overloaded", "try again",
	}
	for _, keyword := range retryableKeywords {
		if strings.Contains(errMsg, keyword) {
			return true
		}
	}

	return false
}

// isOrContains reports whether err is the target error or (for wrapped
// errors) contains it in the tree, using errors.Is semantics but also
// checking the error string for substring matching as a fallback.
func isOrContains(err, target error) bool {
	// errors.Is uses Unwrap, so composite error types that embed sentinels
	// (for example fmt.Errorf("... %w ...", sentinel)) are matched.
	// Since some provider errors may not implement Unwrap, also fall back
	// to substring matching on the target's Error() message.
	if err.Error() == target.Error() {
		return true
	}
	// Simple walk using Unwrap (compatible with Go 1.13+).
	for {
		unwrapped := unwrap(err)
		if unwrapped == nil {
			break
		}
		if unwrapped.Error() == target.Error() || unwrapped == target {
			return true
		}
		err = unwrapped
	}
	return false
}

// unwrap extracts the wrapped error, handling the standard Unwrap() interface.
func unwrap(err error) error {
	type wrapper interface {
		Unwrap() error
	}
	w, ok := err.(wrapper)
	if !ok {
		return nil
	}
	return w.Unwrap()
}

// isQuotaPermanent checks if an ErrQuotaExceeded error is a permanent auth
// failure rather than a transient rate limit.
func isQuotaPermanent(err error) bool {
	errMsg := strings.ToLower(err.Error())
	permanent := []string{"auth", "unauthorized", "forbidden", "invalid key", "payment"}
	for _, kw := range permanent {
		if strings.Contains(errMsg, kw) {
			return true
		}
	}
	return false
}

// sleepContext sleeps for d or until ctx is cancelled, returning any context error.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
