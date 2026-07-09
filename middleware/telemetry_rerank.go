package middleware

import (
	"context"
	"strconv"

	"github.com/samcharles93/ai-sdk/rerank"
	"github.com/samcharles93/ai-sdk/telemetry"
)

// TelemetryRerankMiddleware wraps a rerank.Provider with OpenTelemetry-compatible
// tracing. Each Rerank call creates a span that records the provider name,
// model, document count, and top-n as attributes. Errors are recorded on
// the span before it ends.
type TelemetryRerankMiddleware struct {
	next   rerank.Provider
	tracer telemetry.Tracer
}

// Ensure TelemetryRerankMiddleware implements rerank.Provider.
var _ rerank.Provider = (*TelemetryRerankMiddleware)(nil)

// NewTelemetryRerankMiddleware creates a new telemetry middleware that wraps
// the given rerank provider with tracing.
func NewTelemetryRerankMiddleware(next rerank.Provider, tracer telemetry.Tracer) *TelemetryRerankMiddleware {
	return &TelemetryRerankMiddleware{next: next, tracer: tracer}
}

// Name returns the name of the underlying provider.
func (t *TelemetryRerankMiddleware) Name() string {
	return t.next.Name()
}

// Rerank performs a reranking request wrapped in a span.
func (t *TelemetryRerankMiddleware) Rerank(ctx context.Context, req rerank.Request) (rerank.Response, error) {
	ctx, span := t.tracer.Start(ctx, "rerank.Rerank")
	defer span.End()

	span.SetAttribute("provider.name", t.next.Name())
	span.SetAttribute("model", req.Model)
	span.SetAttribute("documents.count", strconv.Itoa(len(req.Documents)))
	span.SetAttribute("top_n", strconv.Itoa(req.TopN))

	resp, err := t.next.Rerank(ctx, req)
	if err != nil {
		span.RecordError(err)
	}
	return resp, err
}
