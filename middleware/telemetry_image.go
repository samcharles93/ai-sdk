package middleware

import (
	"context"
	"strconv"

	"github.com/samcharles93/ai-sdk/image"
	"github.com/samcharles93/ai-sdk/telemetry"
)

// TelemetryImageMiddleware wraps an image.Provider with OpenTelemetry-compatible
// tracing. Each GenerateImage call creates a span that records the provider
// name, model, and image count as attributes. Errors are recorded on the
// span before it ends.
type TelemetryImageMiddleware struct {
	next   image.Provider
	tracer telemetry.Tracer
}

// Ensure TelemetryImageMiddleware implements image.Provider.
var _ image.Provider = (*TelemetryImageMiddleware)(nil)

// NewTelemetryImageMiddleware creates a new telemetry middleware that wraps
// the given image provider with tracing.
func NewTelemetryImageMiddleware(next image.Provider, tracer telemetry.Tracer) *TelemetryImageMiddleware {
	return &TelemetryImageMiddleware{next: next, tracer: tracer}
}

// Name returns the name of the underlying provider.
func (t *TelemetryImageMiddleware) Name() string {
	return t.next.Name()
}

// GenerateImage performs an image generation request wrapped in a span.
func (t *TelemetryImageMiddleware) GenerateImage(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
	ctx, span := t.tracer.Start(ctx, "image.GenerateImage")
	defer span.End()

	span.SetAttribute("provider.name", t.next.Name())
	span.SetAttribute("model", req.Model)
	span.SetAttribute("n", strconv.Itoa(req.N))

	resp, err := t.next.GenerateImage(ctx, req)
	if err != nil {
		span.RecordError(err)
	}
	return resp, err
}
