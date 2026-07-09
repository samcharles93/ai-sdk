package rerank

import "errors"

var (
	// ErrNoProvider indicates the Client has no underlying Provider configured.
	ErrNoProvider = errors.New("rerank: no provider configured")

	// ErrInvalidRequest indicates the Request is malformed or missing
	// required fields (for example, no Model, empty Query, or empty Documents).
	ErrInvalidRequest = errors.New("rerank: invalid request")

	// ErrProviderUnavailable indicates the upstream provider is temporarily
	// unreachable or returned a transient failure.
	ErrProviderUnavailable = errors.New("rerank: provider unavailable")

	// ErrRateLimited indicates the upstream provider rejected the request
	// due to rate limiting or quota exhaustion.
	ErrRateLimited = errors.New("rerank: rate limited")

	// ErrAuthFailed indicates the provider rejected the supplied credentials.
	ErrAuthFailed = errors.New("rerank: authentication failed")

	// ErrUnsupported indicates the provider does not support a requested
	// capability.
	ErrUnsupported = errors.New("rerank: unsupported operation")
)
