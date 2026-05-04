package middleware

import (
	"context"
	"strconv"

	"github.com/samcharles93/ai-sdk/pkg/embed"
	"github.com/samcharles93/ai-sdk/pkg/telemetry"
)

// TelemetryEmbedMiddleware wraps an embed.Provider with OpenTelemetry-compatible
// tracing. Each Embed call creates a span that records the provider name,
// model, and input count as attributes. Errors are recorded on the span
// before it ends.
type TelemetryEmbedMiddleware struct {
	next   embed.Provider
	tracer telemetry.Tracer
}

// Ensure TelemetryEmbedMiddleware implements embed.Provider.
var _ embed.Provider = (*TelemetryEmbedMiddleware)(nil)

// NewTelemetryEmbedMiddleware creates a new telemetry middleware that wraps
// the given embed provider with tracing.
func NewTelemetryEmbedMiddleware(next embed.Provider, tracer telemetry.Tracer) *TelemetryEmbedMiddleware {
	return &TelemetryEmbedMiddleware{next: next, tracer: tracer}
}

// Name returns the name of the underlying provider.
func (t *TelemetryEmbedMiddleware) Name() string {
	return t.next.Name()
}

// Embed performs an embedding request wrapped in a span.
func (t *TelemetryEmbedMiddleware) Embed(ctx context.Context, req embed.Request) (embed.Response, error) {
	ctx, span := t.tracer.Start(ctx, "embed.Embed")
	defer span.End()

	span.SetAttribute("provider.name", t.next.Name())
	span.SetAttribute("model", req.Model)
	span.SetAttribute("inputs.count", strconv.Itoa(len(req.Inputs)))

	resp, err := t.next.Embed(ctx, req)
	if err != nil {
		span.RecordError(err)
	}
	return resp, err
}
