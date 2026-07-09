package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/speech"
	"github.com/samcharles93/ai-sdk/telemetry"
)

// TelemetrySpeechMiddleware wraps a speech.Provider with OpenTelemetry-compatible
// tracing. Each GenerateSpeech call creates a span that records the provider
// name, model, and voice as attributes. Errors are recorded on the span
// before it ends.
type TelemetrySpeechMiddleware struct {
	next   speech.Provider
	tracer telemetry.Tracer
}

// Ensure TelemetrySpeechMiddleware implements speech.Provider.
var _ speech.Provider = (*TelemetrySpeechMiddleware)(nil)

// NewTelemetrySpeechMiddleware creates a new telemetry middleware that wraps
// the given speech provider with tracing.
func NewTelemetrySpeechMiddleware(next speech.Provider, tracer telemetry.Tracer) *TelemetrySpeechMiddleware {
	return &TelemetrySpeechMiddleware{next: next, tracer: tracer}
}

// Name returns the name of the underlying provider.
func (t *TelemetrySpeechMiddleware) Name() string {
	return t.next.Name()
}

// GenerateSpeech performs a speech generation request wrapped in a span.
func (t *TelemetrySpeechMiddleware) GenerateSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
	ctx, span := t.tracer.Start(ctx, "speech.GenerateSpeech")
	defer span.End()

	span.SetAttribute("provider.name", t.next.Name())
	span.SetAttribute("model", req.Model)
	span.SetAttribute("voice", req.Voice)

	resp, err := t.next.GenerateSpeech(ctx, req)
	if err != nil {
		span.RecordError(err)
	}
	return resp, err
}
