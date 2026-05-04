package middleware

import (
	"context"
	"errors"
	"io"
	"strconv"

	"github.com/samcharles93/ai-sdk/pkg/object"
	"github.com/samcharles93/ai-sdk/pkg/telemetry"
)

// TelemetryObjectMiddleware wraps an object.Provider with OpenTelemetry-compatible
// tracing. Each GenerateObject or StreamObject call creates a span that
// records the provider name, model, and max tokens as attributes. Errors
// are recorded on the span before it ends.
//
// For StreamObject, the span is started at StreamObject time and ended when
// the returned stream is Closed; errors from Next() are recorded on the
// span but do not end it prematurely.
type TelemetryObjectMiddleware struct {
	next   object.Provider
	tracer telemetry.Tracer
}

// Ensure TelemetryObjectMiddleware implements object.Provider.
var _ object.Provider = (*TelemetryObjectMiddleware)(nil)

// NewTelemetryObjectMiddleware creates a new telemetry middleware that wraps
// the given object provider with tracing.
func NewTelemetryObjectMiddleware(next object.Provider, tracer telemetry.Tracer) *TelemetryObjectMiddleware {
	return &TelemetryObjectMiddleware{next: next, tracer: tracer}
}

// Name returns the name of the underlying provider.
func (t *TelemetryObjectMiddleware) Name() string {
	return t.next.Name()
}

// GenerateObject performs a non-streaming object generation wrapped in a span.
func (t *TelemetryObjectMiddleware) GenerateObject(ctx context.Context, req object.Request) (object.ObjectResult, error) {
	ctx, span := t.tracer.Start(ctx, "object.GenerateObject")
	defer span.End()

	setObjectSpanAttributes(span, t.next.Name(), &req)

	resp, err := t.next.GenerateObject(ctx, req)
	if err != nil {
		span.RecordError(err)
	}
	return resp, err
}

// StreamObject performs a streaming object generation. The span is started
// at call time and ended when the returned stream is Closed.
func (t *TelemetryObjectMiddleware) StreamObject(ctx context.Context, req object.Request) (object.ObjectStream, error) {
	ctx, span := t.tracer.Start(ctx, "object.StreamObject")

	setObjectSpanAttributes(span, t.next.Name(), &req)

	stream, err := t.next.StreamObject(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.End()
		return nil, err
	}

	return &telemetryObjectStream{
		ObjectStream: stream,
		span:         span,
	}, nil
}

// telemetryObjectStream wraps an object.ObjectStream to end the span when
// the stream is closed and to record errors from Next.
type telemetryObjectStream struct {
	object.ObjectStream
	span telemetry.Span
}

func (s *telemetryObjectStream) Next(ctx context.Context) (object.ObjectChunk, error) {
	chunk, err := s.ObjectStream.Next(ctx)
	if err != nil {
		// Do not record io.EOF as an error — it's the normal stream completion
		// signal per the object.ObjectStream contract.
		if !errors.Is(err, io.EOF) {
			s.span.RecordError(err)
		}
	}
	return chunk, err
}

func (s *telemetryObjectStream) Close() error {
	defer s.span.End()
	return s.ObjectStream.Close()
}

// setObjectSpanAttributes records common request metadata on the span.
func setObjectSpanAttributes(span telemetry.Span, providerName string, req *object.Request) {
	span.SetAttribute("provider.name", providerName)
	span.SetAttribute("model", req.Model)
	span.SetAttribute("max_tokens", strconv.Itoa(req.MaxTokens))
}
