package core

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/semconv/v1.41.0/genaiconv"
	"go.opentelemetry.io/otel/trace"

	"github.com/samcharles93/ai-sdk/chat"
)

// instrumentationName is the tracer and meter scope, the module path.
const instrumentationName = "github.com/samcharles93/ai-sdk"

// telemetry holds the tracer and gen_ai instruments for one run. Message
// content is never recorded.
type telemetry struct {
	tracer   trace.Tracer
	duration genaiconv.ClientOperationDuration
	tokens   genaiconv.ClientTokenUsage
}

// defaultTelemetry resolves the global providers once. The globals delegate
// lazily, so a provider installed later is still honoured.
var defaultTelemetry = sync.OnceValue(func() *telemetry {
	return newTelemetry(otel.GetTracerProvider(), otel.GetMeterProvider())
})

func newTelemetry(tp trace.TracerProvider, mp metric.MeterProvider) *telemetry {
	meter := mp.Meter(instrumentationName)
	// Constructors fail only on invalid options; the zero instrument is a no-op.
	d, _ := genaiconv.NewClientOperationDuration(meter)
	t, _ := genaiconv.NewClientTokenUsage(meter)
	return &telemetry{tracer: tp.Tracer(instrumentationName), duration: d, tokens: t}
}

func telemetryFor(opts GenerateOptions) *telemetry {
	if opts.TracerProvider == nil && opts.MeterProvider == nil {
		return defaultTelemetry()
	}
	tp, mp := opts.TracerProvider, opts.MeterProvider
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	return newTelemetry(tp, mp)
}

type telemetryKey struct{}

// withTelemetry carries the run's telemetry to tool execution.
func withTelemetry(ctx context.Context, t *telemetry) context.Context {
	return context.WithValue(ctx, telemetryKey{}, t)
}

// modelCall is one in-flight provider Chat or ChatStream call.
type modelCall struct {
	t        *telemetry
	span     trace.Span
	provider genaiconv.ProviderNameAttr
	model    string
	start    time.Time
}

// startModelCall opens a CLIENT span. The returned ctx must be the one passed
// to the provider so its HTTP spans nest under this one.
func (t *telemetry) startModelCall(ctx context.Context, providerName, model string) (context.Context, *modelCall) {
	name := string(genaiconv.OperationNameChat)
	if model != "" {
		name += " " + model
	}
	ctx, span := t.tracer.Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.GenAIOperationNameChat,
			semconv.GenAIProviderNameKey.String(providerName),
			semconv.GenAIRequestModel(model),
		))
	return ctx, &modelCall{t: t, span: span, provider: genaiconv.ProviderNameAttr(providerName), model: model, start: time.Now()}
}

// end records the outcome, usage and metrics, then ends the span.
func (m *modelCall) end(ctx context.Context, responseID, responseModel, finishReason string, usage chat.Usage, err error) {
	defer m.span.End()
	// Metrics must not inherit the span's cancellation.
	ctx = context.WithoutCancel(ctx)
	extra := []attribute.KeyValue{semconv.GenAIRequestModel(m.model)}
	if responseModel != "" {
		m.span.SetAttributes(semconv.GenAIResponseModel(responseModel))
		extra = append(extra, semconv.GenAIResponseModel(responseModel))
	}
	if responseID != "" {
		m.span.SetAttributes(semconv.GenAIResponseID(responseID))
	}
	if finishReason != "" {
		m.span.SetAttributes(semconv.GenAIResponseFinishReasons(finishReason))
	}
	if err != nil {
		m.span.RecordError(err)
		m.span.SetStatus(codes.Error, err.Error())
		extra = append(extra, semconv.ErrorTypeOther)
	} else {
		m.span.SetAttributes(
			semconv.GenAIUsageInputTokens(usage.PromptTokens),
			semconv.GenAIUsageOutputTokens(usage.CompletionTokens),
		)
		op := genaiconv.OperationNameChat
		m.t.tokens.Record(ctx, int64(usage.PromptTokens), op, m.provider, genaiconv.TokenTypeInput, extra...)
		m.t.tokens.Record(ctx, int64(usage.CompletionTokens), op, m.provider, genaiconv.TokenTypeOutput, extra...)
	}
	m.t.duration.Record(ctx, time.Since(m.start).Seconds(), genaiconv.OperationNameChat, m.provider, extra...)
}

// startToolCall opens an INTERNAL execute_tool span. Without telemetry in ctx
// (a direct executeToolCalls call) the returned end function is a no-op.
func startToolCall(ctx context.Context, call ToolCall) (context.Context, func(error)) {
	t, _ := ctx.Value(telemetryKey{}).(*telemetry)
	if t == nil {
		return ctx, func(error) {}
	}
	ctx, span := t.tracer.Start(ctx, "execute_tool "+call.ToolName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			semconv.GenAIOperationNameExecuteTool,
			semconv.GenAIToolName(call.ToolName),
			semconv.GenAIToolCallID(call.ToolCallID),
		))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}
