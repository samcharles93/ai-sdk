package middleware

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/speech"
	"github.com/samcharles93/ai-sdk/telemetry"
)

type stubSpeechStream struct {
	closed bool
}

func (s *stubSpeechStream) Next(context.Context) (speech.SpeechChunk, error) {
	return speech.SpeechChunk{}, io.EOF
}

func (s *stubSpeechStream) Close() error {
	s.closed = true
	return nil
}

type stubSpeechStreamer struct {
	name   string
	stream speech.SpeechStream
	err    error
	calls  int
}

func (s *stubSpeechStreamer) Name() string { return s.name }

func (s *stubSpeechStreamer) StreamSpeech(context.Context, speech.GenerateSpeechRequest) (speech.SpeechStream, error) {
	s.calls++
	return s.stream, s.err
}

type taggingSpeechStreamer struct {
	next  speech.Streamer
	tag   string
	order *[]string
}

func (t *taggingSpeechStreamer) Name() string { return t.next.Name() }

func (t *taggingSpeechStreamer) StreamSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.SpeechStream, error) {
	*t.order = append(*t.order, t.tag)
	return t.next.StreamSpeech(ctx, req)
}

func TestChainSpeechStreamOrder(t *testing.T) {
	var order []string
	mark := func(tag string) SpeechStreamMiddleware {
		return func(next speech.Streamer) speech.Streamer {
			return &taggingSpeechStreamer{next: next, tag: tag, order: &order}
		}
	}

	wrapped := ChainSpeechStream(mark("first"), mark("second"))(&stubSpeechStreamer{name: "base"})
	if _, err := wrapped.StreamSpeech(context.Background(), speech.GenerateSpeechRequest{Text: "hi"}); err != nil {
		t.Fatalf("StreamSpeech: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("order = %v, want [first second]", order)
	}
}

func TestCircuitBreakerSpeechStream(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenTimeout:      time.Minute,
	}
	sentinel := errors.New("boom")
	wrapped := CircuitBreakerSpeechStream(cfg)(&stubSpeechStreamer{name: "base", err: sentinel})

	if _, err := wrapped.StreamSpeech(context.Background(), speech.GenerateSpeechRequest{Text: "hi"}); !errors.Is(err, sentinel) {
		t.Fatalf("first error = %v, want the provider error", err)
	}
	if _, err := wrapped.StreamSpeech(context.Background(), speech.GenerateSpeechRequest{Text: "hi"}); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("second error = %v, want ErrCircuitOpen", err)
	}
}

type recordingSpan struct {
	name   string
	attrs  map[string]string
	errors []error
	ended  bool
}

func (s *recordingSpan) End() { s.ended = true }

func (s *recordingSpan) SetAttribute(key, value string) { s.attrs[key] = value }

func (s *recordingSpan) RecordError(err error) { s.errors = append(s.errors, err) }

type recordingTracer struct {
	spans []*recordingSpan
}

func (t *recordingTracer) Start(_ context.Context, name string) (context.Context, telemetry.Span) {
	span := &recordingSpan{name: name, attrs: map[string]string{}}
	t.spans = append(t.spans, span)
	return context.Background(), span
}

func TestTelemetrySpeechStream(t *testing.T) {
	t.Run("span ends on Close", func(t *testing.T) {
		tracer := &recordingTracer{}
		stream := &stubSpeechStream{}
		wrapped := NewTelemetrySpeechStreamMiddleware(&stubSpeechStreamer{name: "base", stream: stream}, tracer)

		got, err := wrapped.StreamSpeech(context.Background(), speech.GenerateSpeechRequest{Text: "hi", Model: "m", Voice: "v"})
		if err != nil {
			t.Fatalf("StreamSpeech: %v", err)
		}
		if len(tracer.spans) != 1 || tracer.spans[0].name != "speech.StreamSpeech" {
			t.Fatalf("spans = %+v, want one speech.StreamSpeech span", tracer.spans)
		}
		if tracer.spans[0].ended {
			t.Fatal("span ended before Close")
		}
		if tracer.spans[0].attrs["model"] != "m" {
			t.Fatalf("model attribute = %q, want m", tracer.spans[0].attrs["model"])
		}

		if err := got.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if !stream.closed {
			t.Fatal("underlying stream was not closed")
		}
		if !tracer.spans[0].ended {
			t.Fatal("span was not ended on Close")
		}
	})

	t.Run("establishment error ends the span", func(t *testing.T) {
		tracer := &recordingTracer{}
		sentinel := errors.New("boom")
		wrapped := NewTelemetrySpeechStreamMiddleware(&stubSpeechStreamer{name: "base", err: sentinel}, tracer)

		if _, err := wrapped.StreamSpeech(context.Background(), speech.GenerateSpeechRequest{Text: "hi"}); !errors.Is(err, sentinel) {
			t.Fatalf("error = %v, want the provider error", err)
		}
		if len(tracer.spans) != 1 || !tracer.spans[0].ended {
			t.Fatalf("spans = %+v, want one ended span", tracer.spans)
		}
		if len(tracer.spans[0].errors) != 1 {
			t.Fatalf("recorded errors = %v, want one", tracer.spans[0].errors)
		}
	})
}
