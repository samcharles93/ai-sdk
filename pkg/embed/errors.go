package embed

import "errors"

var (
	// ErrNoProvider indicates the Client has no underlying Provider configured.
	ErrNoProvider = errors.New("embed: no provider configured")

	// ErrInvalidRequest indicates the Request is malformed or missing
	// required fields (for example, no Model or no Inputs).
	ErrInvalidRequest = errors.New("embed: invalid request")

	// ErrProviderUnavailable indicates the upstream provider is temporarily
	// unreachable or returned a transient failure.
	ErrProviderUnavailable = errors.New("embed: provider unavailable")

	// ErrRateLimited indicates the upstream provider rejected the request
	// due to rate limiting or quota exhaustion.
	ErrRateLimited = errors.New("embed: rate limited")

	// ErrAuthFailed indicates the provider rejected the supplied credentials.
	ErrAuthFailed = errors.New("embed: authentication failed")

	// ErrUnsupported indicates the provider does not support a requested
	// capability.
	ErrUnsupported = errors.New("embed: unsupported operation")

	// ErrModelMismatch indicates an embedding produced by one model is
	// being mixed with embeddings produced by another. Cosine distances
	// across heterogeneous models are not meaningful, so the SDK refuses
	// the operation rather than silently producing garbage scores.
	ErrModelMismatch = errors.New("embed: embedding model mismatch")

	// ErrDimMismatch indicates an embedding's dimensionality does not
	// match the rest of the index.
	ErrDimMismatch = errors.New("embed: embedding dimension mismatch")
)
