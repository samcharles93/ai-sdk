package middleware

import (
	"context"
	"strconv"

	"github.com/samcharles93/ai-sdk/pkg/telemetry"
	"github.com/samcharles93/ai-sdk/pkg/video"
)

// TelemetryVideoMiddleware wraps a video.Provider with OpenTelemetry-compatible
// tracing. Each GenerateVideo call creates a span that records the provider
// name, model, resolution, and frame rate as attributes. Errors are recorded
// on the span before it ends.
type TelemetryVideoMiddleware struct {
	next   video.Provider
	tracer telemetry.Tracer
}

// Ensure TelemetryVideoMiddleware implements video.Provider.
var _ video.Provider = (*TelemetryVideoMiddleware)(nil)

// NewTelemetryVideoMiddleware creates a new telemetry middleware that wraps
// the given video provider with tracing.
func NewTelemetryVideoMiddleware(next video.Provider, tracer telemetry.Tracer) *TelemetryVideoMiddleware {
	return &TelemetryVideoMiddleware{next: next, tracer: tracer}
}

// Name returns the name of the underlying provider.
func (t *TelemetryVideoMiddleware) Name() string {
	return t.next.Name()
}

// GenerateVideo performs a video generation request wrapped in a span.
func (t *TelemetryVideoMiddleware) GenerateVideo(ctx context.Context, req video.GenerateVideoRequest) (video.GenerateVideoResponse, error) {
	ctx, span := t.tracer.Start(ctx, "video.GenerateVideo")
	defer span.End()

	span.SetAttribute("provider.name", t.next.Name())
	span.SetAttribute("model", req.Model)
	span.SetAttribute("resolution", req.Resolution)
	span.SetAttribute("frame_rate", strconv.Itoa(req.FrameRate))

	resp, err := t.next.GenerateVideo(ctx, req)
	if err != nil {
		span.RecordError(err)
	}
	return resp, err
}
