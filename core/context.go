package core

import "context"

type (
	replayedContextKey       struct{}
	idempotencyKeyContextKey struct{}
)

// WithReplayed returns a context that marks the call as driven from a
// checkpoint journal replay rather than a live provider or tool invocation.
// The checkpointed agent loop attaches it around replayed model and tool
// calls so hooks can tell them apart.
func WithReplayed(ctx context.Context) context.Context {
	return context.WithValue(ctx, replayedContextKey{}, true)
}

// Replayed reports whether ctx marks a replayed (journalled) call. Hooks
// observing a replayed call should not double-count telemetry or cost.
func Replayed(ctx context.Context) bool {
	v, _ := ctx.Value(replayedContextKey{}).(bool)
	return v
}

// WithIdempotencyKey returns a context carrying the idempotency key of the
// current tool call. The checkpointed agent loop derives the key from the
// call's journal position, so it is identical on every replay of the same
// run; tools with side effects can use it to deduplicate.
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, idempotencyKeyContextKey{}, key)
}

// IdempotencyKey returns the idempotency key the agent loop attached to this
// tool call, or "" when the call did not come through a checkpointed loop.
func IdempotencyKey(ctx context.Context) string {
	k, _ := ctx.Value(idempotencyKeyContextKey{}).(string)
	return k
}
