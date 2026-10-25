package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/speech"
)

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
