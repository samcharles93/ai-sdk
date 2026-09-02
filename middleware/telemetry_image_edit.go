package middleware

import (
	"context"
	"strconv"

	"github.com/samcharles93/ai-sdk/image"
	"github.com/samcharles93/ai-sdk/telemetry"
)

// TelemetryImageEditMiddleware wraps an [image.Editor] with OpenTelemetry-compatible
// tracing. Each EditImage call creates a span that records the provider
// name, model, and image count as attributes. Errors are recorded on the
// span before it ends.
type TelemetryImageEditMiddleware struct {
	next   image.Editor
	tracer telemetry.Tracer
}

// Ensure TelemetryImageEditMiddleware implements image.Editor.
var _ image.Editor = (*TelemetryImageEditMiddleware)(nil)

// NewTelemetryImageEditMiddleware creates a new telemetry middleware that
// wraps the given image editor with tracing.
func NewTelemetryImageEditMiddleware(next image.Editor, tracer telemetry.Tracer) *TelemetryImageEditMiddleware {
	return &TelemetryImageEditMiddleware{next: next, tracer: tracer}
}

// Name returns the name of the underlying provider.
func (t *TelemetryImageEditMiddleware) Name() string {
	return t.next.Name()
}

// EditImage performs an image edit request wrapped in a span.
func (t *TelemetryImageEditMiddleware) EditImage(ctx context.Context, req image.EditImageRequest) (image.EditImageResponse, error) {
	ctx, span := t.tracer.Start(ctx, "image.EditImage")
	defer span.End()

	span.SetAttribute("provider.name", t.next.Name())
	span.SetAttribute("model", req.Model)
	span.SetAttribute("n", strconv.Itoa(req.N))

	resp, err := t.next.EditImage(ctx, req)
	if err != nil {
		span.RecordError(err)
	}
	return resp, err
}
