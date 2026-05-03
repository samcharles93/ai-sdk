package middleware

import (
	"context"
	"strconv"

	"github.com/samcharles93/ai-sdk/pkg/chat"
	"github.com/samcharles93/ai-sdk/pkg/telemetry"
)

// TelemetryMiddleware wraps a chat.Provider with OpenTelemetry-compatible
// tracing. Each Chat or ChatStream call creates a span that records the
// provider name, model, message count, and tool count as attributes.
// Errors are recorded on the span before it ends.
//
// For ChatStream, the span is started at ChatStream time and ended when
// the returned stream is Closed; errors from Next() are recorded on the
// span but do not end it prematurely.
type TelemetryMiddleware struct {
	next   chat.Provider
	tracer telemetry.Tracer
}

// Ensure TelemetryMiddleware implements chat.Provider.
var _ chat.Provider = (*TelemetryMiddleware)(nil)

// NewTelemetryMiddleware creates a new telemetry middleware that wraps
// the given provider with tracing.
func NewTelemetryMiddleware(next chat.Provider, tracer telemetry.Tracer) *TelemetryMiddleware {
	return &TelemetryMiddleware{next: next, tracer: tracer}
}

// Name returns the name of the underlying provider.
func (t *TelemetryMiddleware) Name() string {
	return t.next.Name()
}

// Chat performs a non-streaming chat completion wrapped in a span.
func (t *TelemetryMiddleware) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	ctx, span := t.tracer.Start(ctx, "chat.Chat")
	defer span.End()

	setSpanAttributes(span, t.next.Name(), &req)

	resp, err := t.next.Chat(ctx, req)
	if err != nil {
		span.RecordError(err)
	}
	return resp, err
}

// ChatStream performs a streaming chat completion. The span is started at
// call time and ended when the returned stream is Closed.
func (t *TelemetryMiddleware) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	ctx, span := t.tracer.Start(ctx, "chat.ChatStream")

	setSpanAttributes(span, t.next.Name(), &req)

	stream, err := t.next.ChatStream(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.End()
		return nil, err
	}

	return &telemetryStream{
		Stream: stream,
		span:   span,
	}, nil
}

// telemetryStream wraps a chat.Stream to end the span when the stream
// is closed and to record errors from Next.
type telemetryStream struct {
	chat.Stream
	span telemetry.Span
}

func (s *telemetryStream) Next(ctx context.Context) (chat.Chunk, error) {
	chunk, err := s.Stream.Next(ctx)
	if err != nil {
		s.span.RecordError(err)
	}
	return chunk, err
}

func (s *telemetryStream) Close() error {
	defer s.span.End()
	return s.Stream.Close()
}

// setSpanAttributes records common request metadata on the span.
func setSpanAttributes(span telemetry.Span, providerName string, req *chat.Request) {
	span.SetAttribute("provider.name", providerName)
	span.SetAttribute("model", req.Model)
	span.SetAttribute("messages.count", strconv.Itoa(len(req.Messages)))
	span.SetAttribute("tools.count", strconv.Itoa(len(req.Tools)))
}
