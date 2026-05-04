package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/pkg/telemetry"
	"github.com/samcharles93/ai-sdk/pkg/transcribe"
)

// TelemetryTranscribeMiddleware wraps a transcribe.Provider with
// OpenTelemetry-compatible tracing. Each Transcribe call creates a span
// that records the provider name, model, and language as attributes.
// Errors are recorded on the span before it ends.
type TelemetryTranscribeMiddleware struct {
	next   transcribe.Provider
	tracer telemetry.Tracer
}

// Ensure TelemetryTranscribeMiddleware implements transcribe.Provider.
var _ transcribe.Provider = (*TelemetryTranscribeMiddleware)(nil)

// NewTelemetryTranscribeMiddleware creates a new telemetry middleware that
// wraps the given transcribe provider with tracing.
func NewTelemetryTranscribeMiddleware(next transcribe.Provider, tracer telemetry.Tracer) *TelemetryTranscribeMiddleware {
	return &TelemetryTranscribeMiddleware{next: next, tracer: tracer}
}

// Name returns the name of the underlying provider.
func (t *TelemetryTranscribeMiddleware) Name() string {
	return t.next.Name()
}

// Transcribe performs a transcription request wrapped in a span.
func (t *TelemetryTranscribeMiddleware) Transcribe(ctx context.Context, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error) {
	ctx, span := t.tracer.Start(ctx, "transcribe.Transcribe")
	defer span.End()

	span.SetAttribute("provider.name", t.next.Name())
	span.SetAttribute("model", req.Model)
	span.SetAttribute("language", req.Language)

	resp, err := t.next.Transcribe(ctx, req)
	if err != nil {
		span.RecordError(err)
	}
	return resp, err
}
