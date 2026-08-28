package middleware

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/embed"
	errx "github.com/samcharles93/ai-sdk/error"
)

// ---------------------------------------------------------------------------
// DefaultRetryableError: typed classification takes precedence
// ---------------------------------------------------------------------------

func TestDefaultRetryableError_TypedProviderError_UsesRetryableField(t *testing.T) {
	// A ProviderError whose message contains none of the retryable
	// keywords, but whose Retryable field is true — proves the typed
	// path, not string matching, drives the decision.
	base := errors.New("chat: rate limited")
	err := &errx.ProviderError{
		Provider:   "openai",
		StatusCode: 429,
		Retryable:  true,
		Base:       base,
		Message:    "completely unrelated text with no retry keywords",
	}
	if !DefaultRetryableError(err) {
		t.Error("DefaultRetryableError() = false, want true (Retryable field is true)")
	}
}

func TestDefaultRetryableError_TypedProviderError_RetryableFalse_EvenWithRetryKeyword(t *testing.T) {
	// A ProviderError whose message DOES contain a retryable keyword
	// ("timeout"), but whose Retryable field is explicitly false —
	// proves the typed field wins over string matching, in both
	// directions.
	base := errors.New("chat: auth failed")
	err := &errx.ProviderError{
		Provider:   "openai",
		StatusCode: 401,
		Retryable:  false,
		Base:       base,
		Message:    "request timeout while validating credentials",
	}
	if DefaultRetryableError(err) {
		t.Error("DefaultRetryableError() = true, want false (Retryable field is false)")
	}
}

func TestDefaultRetryableError_TypedProviderError_ThroughWrapLayer(t *testing.T) {
	// errors.As must find the ProviderError even when wrapped further
	// by a caller (e.g. fmt.Errorf("%w: additional context", err)).
	perr := &errx.ProviderError{
		Provider:  "anthropic",
		Retryable: true,
		Base:      errors.New("rate limited"),
	}
	wrapped := &testWrapError{inner: perr}

	if !DefaultRetryableError(wrapped) {
		t.Error("DefaultRetryableError() = false, want true through a wrap layer")
	}
}

type testWrapError struct{ inner error }

func (w *testWrapError) Error() string { return "wrapped: " + w.inner.Error() }
func (w *testWrapError) Unwrap() error { return w.inner }

func TestDefaultRetryableError_NonTypedError_FallsBackToStringMatching(t *testing.T) {
	// A plain error (not a ProviderError) must still be classified by
	// the existing sentinel/substring fallback — unchanged behaviour.
	if !DefaultRetryableError(errRetryable) {
		t.Error("DefaultRetryableError(errRetryable) = false, want true (fallback path)")
	}
	if DefaultRetryableError(errNonRetryable) {
		t.Error("DefaultRetryableError(errNonRetryable) = true, want false (fallback path)")
	}
}

// ---------------------------------------------------------------------------
// effectiveDelay: Retry-After precedence
// ---------------------------------------------------------------------------

func TestEffectiveDelay_PrefersProviderErrorRetryAfter(t *testing.T) {
	backoff := fixedBackoff{d: 100 * time.Millisecond}
	err := &errx.ProviderError{RetryAfter: 30 * time.Second}

	got := effectiveDelay(backoff, 0, err)
	if got != 30*time.Second {
		t.Errorf("effectiveDelay() = %v, want 30s (server Retry-After)", got)
	}
}

func TestEffectiveDelay_FallsBackToBackoff_WhenNoRetryAfter(t *testing.T) {
	backoff := fixedBackoff{d: 250 * time.Millisecond}
	err := &errx.ProviderError{RetryAfter: 0} // absent/unparseable

	got := effectiveDelay(backoff, 0, err)
	if got != 250*time.Millisecond {
		t.Errorf("effectiveDelay() = %v, want 250ms (computed backoff)", got)
	}
}

func TestEffectiveDelay_FallsBackToBackoff_ForNonProviderError(t *testing.T) {
	backoff := fixedBackoff{d: 500 * time.Millisecond}

	got := effectiveDelay(backoff, 0, errRetryable)
	if got != 500*time.Millisecond {
		t.Errorf("effectiveDelay() = %v, want 500ms (computed backoff)", got)
	}
}

type fixedBackoff struct{ d time.Duration }

func (b fixedBackoff) Backoff(_ int) time.Duration { return b.d }

// ---------------------------------------------------------------------------
// RetryConfig.OnAttempt observer
// ---------------------------------------------------------------------------

type attemptRecord struct {
	attempt int
	err     error
	delay   time.Duration
}

func TestRetryChat_OnAttempt_FiresOncePerRetryableFailure(t *testing.T) {
	const maxAttempts = 3
	p := &countingChatProvider{
		name: "always-fail",
		chatFn: func(_ context.Context, _ chat.Request) (chat.Response, error) {
			return chat.Response{}, errRetryable
		},
	}

	var records []attemptRecord
	cfg := RetryConfig{
		MaxAttempts: maxAttempts,
		OnAttempt: func(attempt int, err error, delay time.Duration) {
			records = append(records, attemptRecord{attempt, err, delay})
		},
	}

	mw := RetryChat(cfg, zeroBackoff{}, alwaysRetryable)(p)
	_, _ = mw.Chat(context.Background(), chat.Request{Model: "m"})

	// Observer fires on every retryable failure, including the final
	// (non-retried) attempt: exactly maxAttempts times.
	if len(records) != maxAttempts {
		t.Fatalf("OnAttempt fired %d times, want %d", len(records), maxAttempts)
	}
	for i, r := range records {
		if r.attempt != i {
			t.Errorf("records[%d].attempt = %d, want %d", i, r.attempt, i)
		}
		if !errors.Is(r.err, errRetryable) {
			t.Errorf("records[%d].err = %v, want errRetryable", i, r.err)
		}
	}
}

func TestRetryChat_OnAttempt_NotCalledOnSuccess(t *testing.T) {
	p := &countingChatProvider{
		name: "ok",
		chatFn: func(_ context.Context, _ chat.Request) (chat.Response, error) {
			return chat.Response{Content: "hi"}, nil
		},
	}

	var calls int
	cfg := RetryConfig{
		MaxAttempts: 5,
		OnAttempt:   func(_ int, _ error, _ time.Duration) { calls++ },
	}

	mw := RetryChat(cfg, zeroBackoff{}, alwaysRetryable)(p)
	if _, err := mw.Chat(context.Background(), chat.Request{Model: "m"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if calls != 0 {
		t.Errorf("OnAttempt called %d times, want 0 on first-attempt success", calls)
	}
}

func TestRetryChat_OnAttempt_NotCalledForNonRetryableError(t *testing.T) {
	p := &countingChatProvider{
		name: "auth-fail",
		chatFn: func(_ context.Context, _ chat.Request) (chat.Response, error) {
			return chat.Response{}, errNonRetryable
		},
	}

	var calls int
	cfg := RetryConfig{
		MaxAttempts: 5,
		OnAttempt:   func(_ int, _ error, _ time.Duration) { calls++ },
	}

	mw := RetryChat(cfg, zeroBackoff{}, neverRetryable)(p)
	_, _ = mw.Chat(context.Background(), chat.Request{Model: "m"})

	if calls != 0 {
		t.Errorf("OnAttempt called %d times, want 0 (retryable() returned false, loop short-circuits before OnAttempt)", calls)
	}
}

func TestRetryChat_OnAttempt_ReceivesProviderErrorDelay(t *testing.T) {
	// End-to-end: a ProviderError with RetryAfter set flows through
	// effectiveDelay into the OnAttempt callback.
	perr := &errx.ProviderError{Retryable: true, RetryAfter: 42 * time.Millisecond, Base: errors.New("rate limited")}
	p := &countingChatProvider{
		name: "rate-limited",
		chatFn: func(_ context.Context, _ chat.Request) (chat.Response, error) {
			return chat.Response{}, perr
		},
	}

	var gotDelay time.Duration
	cfg := RetryConfig{
		MaxAttempts: 2,
		OnAttempt: func(_ int, _ error, delay time.Duration) {
			gotDelay = delay
		},
	}

	// Backoff would return 999s if consulted — proves RetryAfter won.
	mw := RetryChat(cfg, fixedBackoff{d: 999 * time.Second}, DefaultRetryableError)(p)
	_, _ = mw.Chat(context.Background(), chat.Request{Model: "m"})

	if gotDelay != 42*time.Millisecond {
		t.Errorf("OnAttempt delay = %v, want 42ms (from ProviderError.RetryAfter)", gotDelay)
	}
}

// ---------------------------------------------------------------------------
// OnAttempt propagates uniformly to the non-chat/object retry domains too
// (embed is representative of the 6 single-method domains, all updated
// with the same mechanical loop-body change).
// ---------------------------------------------------------------------------

// countingEmbedProvider is a controllable embed.Provider for this test,
// local to this file since middleware's existing fixtures only cover
// chat/object.
type countingEmbedProvider struct {
	calls atomic.Int64
	fn    func(ctx context.Context, req embed.Request) (embed.Response, error)
}

func (p *countingEmbedProvider) Name() string { return "counting-embed" }

func (p *countingEmbedProvider) Embed(ctx context.Context, req embed.Request) (embed.Response, error) {
	p.calls.Add(1)
	return p.fn(ctx, req)
}

func TestRetryEmbed_OnAttempt_FiresOncePerRetryableFailure(t *testing.T) {
	const maxAttempts = 3
	p := &countingEmbedProvider{
		fn: func(_ context.Context, _ embed.Request) (embed.Response, error) {
			return embed.Response{}, errRetryable
		},
	}

	var calls int
	cfg := RetryConfig{
		MaxAttempts: maxAttempts,
		OnAttempt:   func(_ int, _ error, _ time.Duration) { calls++ },
	}

	mw := RetryEmbed(cfg, zeroBackoff{}, alwaysRetryable)(p)
	_, _ = mw.Embed(context.Background(), embed.Request{Model: "m", Inputs: []string{"x"}})

	if calls != maxAttempts {
		t.Errorf("OnAttempt fired %d times, want %d", calls, maxAttempts)
	}
	if p.calls.Load() != int64(maxAttempts) {
		t.Errorf("Embed call count = %d, want %d", p.calls.Load(), maxAttempts)
	}
}
