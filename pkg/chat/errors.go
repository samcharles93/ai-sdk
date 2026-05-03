package chat

import (
	"errors"
	"fmt"
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
