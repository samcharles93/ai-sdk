package errx

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter_DeltaSeconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "5")

	got := ParseRetryAfter(h)
	if got != 5*time.Second {
		t.Errorf("ParseRetryAfter() = %v, want 5s", got)
	}
}

func TestParseRetryAfter_HTTPDate_Future(t *testing.T) {
	future := time.Now().Add(30 * time.Second)
	h := http.Header{}
	h.Set("Retry-After", future.UTC().Format(http.TimeFormat))

	got := ParseRetryAfter(h)
	// Allow a little slack for clock/formatting rounding to whole seconds.
	if got < 28*time.Second || got > 31*time.Second {
		t.Errorf("ParseRetryAfter() = %v, want ~30s", got)
	}
}

func TestParseRetryAfter_HTTPDate_Past(t *testing.T) {
	past := time.Now().Add(-30 * time.Second)
	h := http.Header{}
	h.Set("Retry-After", past.UTC().Format(http.TimeFormat))

	got := ParseRetryAfter(h)
	if got != 0 {
		t.Errorf("ParseRetryAfter() = %v, want 0 for a past HTTP-date", got)
	}
}

func TestParseRetryAfter_MsVariant(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After-Ms", "1500")

	got := ParseRetryAfter(h)
	if got != 1500*time.Millisecond {
		t.Errorf("ParseRetryAfter() = %v, want 1500ms", got)
	}
}

func TestParseRetryAfter_Absent(t *testing.T) {
	h := http.Header{}
	if got := ParseRetryAfter(h); got != 0 {
		t.Errorf("ParseRetryAfter() = %v, want 0 for absent header", got)
	}
}

func TestParseRetryAfter_Malformed(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "not-a-number-or-date")
	if got := ParseRetryAfter(h); got != 0 {
		t.Errorf("ParseRetryAfter() = %v, want 0 for malformed header", got)
	}
}

func TestParseRetryAfter_PrefersRetryAfterOverMsVariant(t *testing.T) {
	// When both are present, the standard header wins.
	h := http.Header{}
	h.Set("Retry-After", "10")
	h.Set("Retry-After-Ms", "1")

	got := ParseRetryAfter(h)
	if got != 10*time.Second {
		t.Errorf("ParseRetryAfter() = %v, want 10s (Retry-After takes precedence)", got)
	}
}

func TestNewProviderError_PopulatesFields(t *testing.T) {
	base := errors.New("rate limited")
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
	}
	resp.Header.Set("Retry-After", "3")
	resp.Header.Set("X-Request-Id", "req-123")

	err := NewProviderError("openai", resp, base, "too many requests", true)

	if err.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", err.Provider)
	}
	if err.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want %d", err.StatusCode, http.StatusTooManyRequests)
	}
	if err.RequestID != "req-123" {
		t.Errorf("RequestID = %q, want req-123", err.RequestID)
	}
	if err.RetryAfter != 3*time.Second {
		t.Errorf("RetryAfter = %v, want 3s", err.RetryAfter)
	}
	if !err.Retryable {
		t.Error("Retryable = false, want true")
	}
	if err.Message != "too many requests" {
		t.Errorf("Message = %q, want %q", err.Message, "too many requests")
	}
}

func TestNewProviderError_RequestIDFallsBackToRequestIdHeader(t *testing.T) {
	resp := &http.Response{StatusCode: 500, Header: http.Header{}}
	resp.Header.Set("Request-Id", "req-456")

	err := NewProviderError("anthropic", resp, errors.New("boom"), "", false)

	if err.RequestID != "req-456" {
		t.Errorf("RequestID = %q, want req-456", err.RequestID)
	}
}

func TestNewProviderError_ErrorsIsMatchesBase(t *testing.T) {
	base := errors.New("auth failed")
	resp := &http.Response{StatusCode: 401, Header: http.Header{}}

	err := NewProviderError("openai", resp, base, "unauthorised", false)

	if !errors.Is(err, base) {
		t.Error("errors.Is(err, base) = false, want true")
	}
}

func TestNewProviderError_ErrorsAsMatchesProviderError(t *testing.T) {
	base := errors.New("rate limited")
	resp := &http.Response{StatusCode: 429, Header: http.Header{}}
	wrapped := errorsWrap(NewProviderError("groq", resp, base, "slow down", true))

	var perr *ProviderError
	if !errors.As(wrapped, &perr) {
		t.Fatal("errors.As failed to find *ProviderError")
	}
	if perr.Provider != "groq" {
		t.Errorf("Provider = %q, want groq", perr.Provider)
	}
}

// errorsWrap simulates a caller wrapping the ProviderError further (for
// example with fmt.Errorf("%w: additional context", err)), to prove
// errors.As still finds it through a wrap layer.
func errorsWrap(err error) error {
	return &wrappedError{inner: err}
}

type wrappedError struct{ inner error }

func (w *wrappedError) Error() string { return "wrapped: " + w.inner.Error() }
func (w *wrappedError) Unwrap() error { return w.inner }

func TestProviderError_ErrorString(t *testing.T) {
	base := errors.New("chat: rate limited")
	resp := &http.Response{StatusCode: 429, Header: http.Header{}}
	err := NewProviderError("openai", resp, base, "slow down", true)

	want := "openai: status 429: slow down: chat: rate limited"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
