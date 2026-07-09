package object

import (
	"errors"
	"fmt"
)

var (
	// ErrNoProvider indicates the Client has no underlying Provider configured.
	ErrNoProvider = errors.New("object: no provider configured")

	// ErrInvalidRequest indicates the Request is malformed or missing
	// required fields.
	ErrInvalidRequest = errors.New("object: invalid request")

	// ErrProviderUnavailable indicates the upstream provider is temporarily
	// unreachable or returned a transient failure.
	ErrProviderUnavailable = errors.New("object: provider unavailable")

	// ErrRateLimited indicates the upstream provider rejected the request due
	// to rate limiting or quota exhaustion.
	ErrRateLimited = errors.New("object: rate limited")

	// ErrAuthFailed indicates the provider rejected the supplied credentials.
	ErrAuthFailed = errors.New("object: authentication failed")

	// ErrUnsupported indicates the provider does not support a requested
	// capability.
	ErrUnsupported = errors.New("object: unsupported operation")
)

// UnsupportedContentError is an optional typed error carrying context about
// unsupported content. It unwraps to ErrUnsupported to allow errors.Is.
type UnsupportedContentError struct {
	Provider string
	Detail   string
}

func (e *UnsupportedContentError) Error() string {
	return fmt.Sprintf("object: %s: %s", e.Provider, e.Detail)
}

func (e *UnsupportedContentError) Unwrap() error { return ErrUnsupported }
