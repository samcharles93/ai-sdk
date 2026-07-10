package chat

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnsupportedContent indicates a Part kind is not supported by the
// provider/model for the current request. Providers wrap this with
// context (provider, model, part type) when rejecting content.
var ErrUnsupportedContent = errors.New("chat: unsupported content")

// UnsupportedContentError is the typed form of [ErrUnsupportedContent]
// carrying enough context to compose a useful error message and to
// be matched via errors.As. It always errors.Is matches
// [ErrUnsupportedContent].
type UnsupportedContentError struct {
	Provider string
	Model    string
	PartType PartType
}

// Error implements error.
func (e *UnsupportedContentError) Error() string {
	return fmt.Sprintf("chat: %s model %q does not support %s parts", e.Provider, e.Model, e.PartType)
}

// Unwrap allows errors.Is(err, ErrUnsupportedContent) matching.
func (e *UnsupportedContentError) Unwrap() error { return ErrUnsupportedContent }

var (
	// ErrNoProvider indicates the Client has no underlying Provider configured.
	ErrNoProvider = errors.New("chat: no provider configured")

	// ErrInvalidRequest indicates the Request is malformed or missing
	// required fields (for example, no Model or no Messages).
	ErrInvalidRequest = errors.New("chat: invalid request")

	// ErrProviderUnavailable indicates the upstream provider is temporarily
	// unreachable or returned a transient failure.
	ErrProviderUnavailable = errors.New("chat: provider unavailable")

	// ErrRateLimited indicates the upstream provider rejected the request
	// due to rate limiting or quota exhaustion.
	ErrRateLimited = errors.New("chat: rate limited")

	// ErrAuthFailed indicates the provider rejected the supplied credentials.
	ErrAuthFailed = errors.New("chat: authentication failed")

	// ErrContextLength indicates the request exceeds the model's maximum
	// supported context length.
	ErrContextLength = errors.New("chat: context length exceeded")

	// ErrUnsupported indicates the provider does not support a requested
	// capability (for example, streaming or tool calls).
	ErrUnsupported = errors.New("chat: unsupported operation")
)

// maxErrorBodySnippet bounds how much of an HTTP error response body a
// provider embeds in an error's text. This is independent of the byte
// limit used when reading the body (which only bounds memory) — a body
// read within that limit can still be large enough, once stringified into
// an error message, to flood a caller's UI (e.g. an edge/gateway HTML
// error page).
const maxErrorBodySnippet = 300

// SanitizeErrorBody trims an HTTP error response body down to a short,
// display-safe snippet for embedding in an error message. HTML bodies
// (edge/gateway error pages such as Cloudflare's 502/504 pages, which can
// run to several KB of markup) are collapsed to a short marker instead of
// being embedded verbatim — the status code already conveys the failure,
// and embedding raw HTML has repeatedly overwhelmed caller UIs that print
// error text directly to a terminal or notification banner. Non-HTML
// bodies are truncated to maxErrorBodySnippet bytes with a marker appended
// when trimmed.
func SanitizeErrorBody(body []byte) string {
	s := strings.TrimSpace(string(body))
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") {
		return fmt.Sprintf("(html error page, %d bytes)", len(s))
	}
	if len(s) > maxErrorBodySnippet {
		return s[:maxErrorBodySnippet] + "… (truncated)"
	}
	return s
}
