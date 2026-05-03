package speech

import "errors"

var (
	// ErrNoProvider indicates the Client has no underlying Provider configured.
	ErrNoProvider = errors.New("speech: no provider configured")

	// ErrInvalidRequest indicates the Request is malformed or missing
	// required fields (for example, no Model or no Text).
	ErrInvalidRequest = errors.New("speech: invalid request")

	// ErrProviderUnavailable indicates the upstream provider is temporarily
	// unreachable or returned a transient failure.
	ErrProviderUnavailable = errors.New("speech: provider unavailable")

	// ErrRateLimited indicates the upstream provider rejected the request
	// due to rate limiting or quota exhaustion.
	ErrRateLimited = errors.New("speech: rate limited")

	// ErrAuthFailed indicates the provider rejected the supplied credentials.
	ErrAuthFailed = errors.New("speech: authentication failed")

	// ErrUnsupported indicates the provider does not support a requested
	// capability (for example, a specific voice or format).
	ErrUnsupported = errors.New("speech: unsupported operation")
)
