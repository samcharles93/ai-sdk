package telemetry

import "context"

// Span represents a unit of work for tracing. Implementations may attach
// additional metadata and export the span to an external system. Keep the
// surface area intentionally small so middleware can adapt OTel or other
// tracers to this interface.
type Span interface {
	// End finishes the span and flushes any pending data.
	End()

	// SetAttribute sets a string attribute on the span.
	SetAttribute(key, value string)

	// RecordError records an error on the span.
	RecordError(err error)
}

// SpanContext is an opaque carrier for span context propagation. Concrete
// tracer implementations may provide a richer implementation but consumers
// should treat it as opaque.
// SpanContext is an opaque carrier for span context propagation. Concrete
// tracer implementations may provide a richer implementation but consumers
// should treat it as opaque.
type SpanContext interface{ any }

// Tracer is a minimal trace provider. Start creates a new span with the
// provided name and returns a context containing the span plus the Span
// itself. Implementations should ensure the returned context contains any
// required propagation information.
type Tracer interface {
	Start(ctx context.Context, name string) (context.Context, Span)
}

// StartSpanFunc is a function type that mirrors Tracer.Start. Some callers
// prefer passing a function instead of a full Tracer implementation.
type StartSpanFunc func(ctx context.Context, name string) (context.Context, Span)

// NoopSpan is a span that performs no operations. Use when telemetry is
// disabled to avoid nil checks in call sites.
type NoopSpan struct{}

// Ensure NoopSpan implements Span.
var _ Span = (*NoopSpan)(nil)

func (NoopSpan) End()                           {}
func (NoopSpan) SetAttribute(key, value string) {}
func (NoopSpan) RecordError(err error)          {}

// NoopTracer returns a tracer that creates NoopSpan instances.
type NoopTracer struct{}

var _ Tracer = (*NoopTracer)(nil)

func (NoopTracer) Start(ctx context.Context, _ string) (context.Context, Span) {
	// Return the original context and a NoopSpan.
	return ctx, NoopSpan{}
}

// DefaultTracer is the zero-value tracer used when no tracer is configured.
var DefaultTracer Tracer = NoopTracer{}
