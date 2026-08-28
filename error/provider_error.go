package errx

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ProviderError is a typed HTTP-backed provider failure carrying enough
// structure for callers (notably middleware's retry logic) to make
// decisions without re-parsing the error message. It always unwraps to
// Base, so existing errors.Is(err, chat.ErrRateLimited)-style checks
// written before this type existed continue to work unchanged.
type ProviderError struct {
	// Provider identifies the source ("openai", "anthropic", ...).
	Provider string
	// StatusCode is the HTTP status code the provider returned.
	StatusCode int
	// RequestID is the provider's request identifier, when sent.
	// Empty when the provider didn't send one.
	RequestID string
	// RetryAfter is the provider's requested retry delay, parsed from
	// a Retry-After (or Retry-After-Ms) response header. Zero when
	// absent or unparseable.
	RetryAfter time.Duration
	// Retryable is computed once at the HTTP boundary from the status
	// code (and, for some providers, the body), so callers don't have
	// to re-derive it from the error message.
	Retryable bool
	// Base is the domain sentinel this error represents (for example
	// chat.ErrRateLimited). Base.
	Base error
	// Message is a sanitised snippet of the response body.
	Message string
}

// Error implements error, reproducing the "<provider>: status %d: %s: %s"
// shape providers used to build by hand with fmt.Errorf, so existing
// log lines and error-string assertions are unaffected by this type's
// introduction.
func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s: status %d: %s: %s", e.Provider, e.StatusCode, e.Message, e.Base.Error())
}

// Unwrap returns Base, so errors.Is(err, chat.ErrRateLimited) and
// similar sentinel checks continue to work against a *ProviderError.
func (e *ProviderError) Unwrap() error { return e.Base }

// NewProviderError builds a ProviderError from an HTTP response, a
// pre-classified base sentinel, a sanitised message, and whether this
// class of failure should be retried. It reads Retry-After and a
// request-id header off resp.Header; resp.Body is not consumed or
// closed — callers are expected to have already read (and are
// responsible for closing) the body themselves.
func NewProviderError(provider string, resp *http.Response, base error, message string, retryable bool) *ProviderError {
	return &ProviderError{
		Provider:   provider,
		StatusCode: resp.StatusCode,
		RequestID:  firstHeader(resp.Header, "X-Request-Id", "Request-Id"),
		RetryAfter: ParseRetryAfter(resp.Header),
		Retryable:  retryable,
		Base:       base,
		Message:    message,
	}
}

// ParseRetryAfter reads the Retry-After header (delta-seconds or
// HTTP-date, per RFC 7231 §7.1.3) or the non-standard Retry-After-Ms
// header (integer milliseconds, used by some providers for sub-second
// precision) and returns the resulting duration. Returns 0 when
// neither header is present or parseable, or when a parsed HTTP-date
// is already in the past.
func ParseRetryAfter(h http.Header) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
		if t, err := http.ParseTime(v); err == nil {
			if d := time.Until(t); d > 0 {
				return d
			}
			return 0
		}
	}
	if v := h.Get("Retry-After-Ms"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms >= 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 0
}

// firstHeader returns the value of the first present header in keys,
// or "" if none are set. http.Header.Get canonicalises the key it is
// given, so this only needs one lookup per distinct header name, not
// per casing variant of the same name.
func firstHeader(h http.Header, keys ...string) string {
	for _, k := range keys {
		if v := h.Get(k); v != "" {
			return v
		}
	}
	return ""
}
