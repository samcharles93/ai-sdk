package video

import "errors"

var (
    // ErrNoProvider indicates the Client has no underlying Provider configured.
    ErrNoProvider = errors.New("video: no provider configured")

    // ErrInvalidRequest indicates the Request is malformed or missing
    // required fields (for example, no Model or no Prompt).
    ErrInvalidRequest = errors.New("video: invalid request")

    // ErrProviderUnavailable indicates the upstream provider is temporarily
    // unreachable or returned a transient failure.
    ErrProviderUnavailable = errors.New("video: provider unavailable")

    // ErrRateLimited indicates the upstream provider rejected the request
    // due to rate limiting or quota exhaustion.
    ErrRateLimited = errors.New("video: rate limited")

    // ErrAuthFailed indicates the provider rejected the supplied credentials.
    ErrAuthFailed = errors.New("video: authentication failed")
)
