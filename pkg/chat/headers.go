package chat

import (
	"context"
	"maps"
)

// WithContextHeaders attaches extra HTTP headers to a context so that
// provider implementations can inject them into outbound requests.
// Multiple callers can attach headers; later calls overwrite earlier
// values for the same key.
func WithContextHeaders(ctx context.Context, headers map[string]string) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	// Merge with existing headers if any.
	if existing, ok := ctx.Value(contextHeadersKey{}).(map[string]string); ok && len(existing) > 0 {
		merged := make(map[string]string, len(existing)+len(headers))
		maps.Copy(merged, existing)
		maps.Copy(merged, headers)
		return context.WithValue(ctx, contextHeadersKey{}, merged)
	}
	return context.WithValue(ctx, contextHeadersKey{}, headers)
}

// ContextHeaders returns the extra headers stored in ctx, or nil if none
// were set.
func ContextHeaders(ctx context.Context) (map[string]string, bool) {
	h, ok := ctx.Value(contextHeadersKey{}).(map[string]string)
	return h, ok
}

type contextHeadersKey struct{}
