package core

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

type streamingSpeechProvider struct {
	mockSpeechProvider

	stream speech.SpeechStream
	err    error
}

func (m *streamingSpeechProvider) StreamSpeech(context.Context, speech.GenerateSpeechRequest) (speech.SpeechStream, error) {
	return m.stream, m.err
}

type emptySpeechStream struct{}

func (emptySpeechStream) Next(context.Context) (speech.SpeechChunk, error) {
	return speech.SpeechChunk{}, io.EOF
}

func (emptySpeechStream) Close() error { return nil }

func TestStreamSpeech(t *testing.T) {
	req := speech.GenerateSpeechRequest{Model: "m", Text: "hi"}

	t.Run("nil provider", func(t *testing.T) {
		if _, err := StreamSpeech(context.Background(), nil, req); !errors.Is(err, ErrNoProvider) {
			t.Fatalf("error = %v, want ErrNoProvider", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		provider := &streamingSpeechProvider{stream: emptySpeechStream{}}
		if _, err := StreamSpeech(ctx, provider, req); !errors.Is(err, ErrAborted) {
			t.Fatalf("error = %v, want ErrAborted", err)
		}
	})

	t.Run("provider without streamer", func(t *testing.T) {
		provider := &mockSpeechProvider{name: "buffered", fn: func(context.Context, speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
			return speech.GenerateSpeechResponse{}, nil
		}}
		if _, err := StreamSpeech(context.Background(), provider, req); !errors.Is(err, speech.ErrStreamNotSupported) {
			t.Fatalf("error = %v, want ErrStreamNotSupported", err)
		}
	})

	t.Run("establishment error is wrapped", func(t *testing.T) {
		sentinel := errors.New("boom")
		provider := &streamingSpeechProvider{err: sentinel}
		if _, err := StreamSpeech(context.Background(), provider, req); !errors.Is(err, sentinel) {
			t.Fatalf("error = %v, want the provider error", err)
		}
	})

	t.Run("returns the provider stream", func(t *testing.T) {
		stream := emptySpeechStream{}
		provider := &streamingSpeechProvider{stream: stream}
		got, err := StreamSpeech(context.Background(), provider, req)
		if err != nil {
			t.Fatalf("StreamSpeech: %v", err)
		}
		if got == nil {
			t.Fatal("expected a stream")
		}
	})
}
