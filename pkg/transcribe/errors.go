package transcribe

import "errors"

var (
	// ErrNoProvider indicates the Client has no underlying Provider configured.
	ErrNoProvider = errors.New("transcribe: no provider configured")

	// ErrInvalidRequest indicates the Request is malformed or missing
	// required fields (for example, no Model or no Audio).
	ErrInvalidRequest = errors.New("transcribe: invalid request")

	// ErrProviderUnavailable indicates the upstream provider is temporarily
	// unreachable or returned a transient failure.
	ErrProviderUnavailable = errors.New("transcribe: provider unavailable")

	// ErrRateLimited indicates the upstream provider rejected the request
	// due to rate limiting or quota exhaustion.
	ErrRateLimited = errors.New("transcribe: rate limited")

	// ErrAuthFailed indicates the provider rejected the supplied credentials.
	ErrAuthFailed = errors.New("transcribe: authentication failed")

	// ErrUnsupported indicates the provider does not support a requested
	// capability (for example, a specific language or format).
	ErrUnsupported = errors.New("transcribe: unsupported operation")
)
