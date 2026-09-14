package middleware

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/samcharles93/ai-sdk/speech"
	"github.com/samcharles93/ai-sdk/telemetry"
)

// TelemetrySpeechStreamMiddleware wraps a speech.Streamer with
// OpenTelemetry-compatible tracing. One span is started per StreamSpeech call
// and ended when the returned stream is Closed (or when establishment fails).
type TelemetrySpeechStreamMiddleware struct {
	next   speech.Streamer
	tracer telemetry.Tracer
}

// Ensure TelemetrySpeechStreamMiddleware implements speech.Streamer.
var _ speech.Streamer = (*TelemetrySpeechStreamMiddleware)(nil)

// NewTelemetrySpeechStreamMiddleware creates a telemetry middleware that wraps
// the given speech streamer with tracing.
func NewTelemetrySpeechStreamMiddleware(next speech.Streamer, tracer telemetry.Tracer) *TelemetrySpeechStreamMiddleware {
	return &TelemetrySpeechStreamMiddleware{next: next, tracer: tracer}
}

// Name returns the name of the underlying provider.
func (t *TelemetrySpeechStreamMiddleware) Name() string {
	return t.next.Name()
}

// StreamSpeech starts a streaming synthesis wrapped in a span.
func (t *TelemetrySpeechStreamMiddleware) StreamSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.SpeechStream, error) {
	ctx, span := t.tracer.Start(ctx, "speech.StreamSpeech")
	span.SetAttribute("provider.name", t.next.Name())
	span.SetAttribute("model", req.Model)
	span.SetAttribute("voice", req.Voice)

	stream, err := t.next.StreamSpeech(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.End()
		return nil, err
	}
	return &telemetrySpeechStream{next: stream, span: span}, nil
}

// telemetrySpeechStream ends the span exactly once, on Close or on the first
// terminal stream error.
type telemetrySpeechStream struct {
	next speech.SpeechStream
	span telemetry.Span
	once sync.Once
}

func (s *telemetrySpeechStream) Next(ctx context.Context) (speech.SpeechChunk, error) {
	chunk, err := s.next.Next(ctx)
	if err != nil && !errors.Is(err, io.EOF) {
		s.span.RecordError(err)
		s.end()
	}
	return chunk, err
}

func (s *telemetrySpeechStream) Close() error {
	err := s.next.Close()
	if err != nil {
		s.span.RecordError(err)
	}
	s.end()
	return err
}

func (s *telemetrySpeechStream) end() {
	s.once.Do(s.span.End)
}
