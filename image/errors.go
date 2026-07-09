package image

import "errors"

var (
	// ErrNoProvider indicates the Client has no underlying Provider configured.
	ErrNoProvider = errors.New("image: no provider configured")

	// ErrInvalidRequest indicates the Request is malformed or missing
	// required fields (for example, no Model or no Prompt).
	ErrInvalidRequest = errors.New("image: invalid request")

	// ErrProviderUnavailable indicates the upstream provider is temporarily
	// unreachable or returned a transient failure.
	ErrProviderUnavailable = errors.New("image: provider unavailable")

	// ErrRateLimited indicates the upstream provider rejected the request
	// due to rate limiting or quota exhaustion.
	ErrRateLimited = errors.New("image: rate limited")

	// ErrAuthFailed indicates the provider rejected the supplied credentials.
	ErrAuthFailed = errors.New("image: authentication failed")

	// ErrContentFiltered indicates the provider rejected the prompt due to
	// content filtering.
	ErrContentFiltered = errors.New("image: content filtered")
)
