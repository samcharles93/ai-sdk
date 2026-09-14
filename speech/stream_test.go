package speech

import (
	"context"
	"errors"
	"io"
	"testing"
)

type fakeStream struct {
	closed bool
}

func (s *fakeStream) Next(context.Context) (SpeechChunk, error) {
	return SpeechChunk{}, io.EOF
}

func (s *fakeStream) Close() error {
	s.closed = true
	return nil
}

type fakeStreamer struct {
	fakeProvider

	stream SpeechStream
	err    error
	calls  int
}

func (f *fakeStreamer) StreamSpeech(context.Context, GenerateSpeechRequest) (SpeechStream, error) {
	f.calls++
	return f.stream, f.err
}

func TestClientStreamSpeech(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		var c *Client
		if _, err := c.StreamSpeech(context.Background(), GenerateSpeechRequest{Text: "hi"}); !errors.Is(err, ErrNoProvider) {
			t.Fatalf("error = %v, want ErrNoProvider", err)
		}
	})

	t.Run("nil provider", func(t *testing.T) {
		c := NewClient(nil)
		if _, err := c.StreamSpeech(context.Background(), GenerateSpeechRequest{Text: "hi"}); !errors.Is(err, ErrNoProvider) {
			t.Fatalf("error = %v, want ErrNoProvider", err)
		}
	})

	t.Run("empty text", func(t *testing.T) {
		c := NewClient(&fakeStreamer{})
		if _, err := c.StreamSpeech(context.Background(), GenerateSpeechRequest{}); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("error = %v, want ErrInvalidRequest", err)
		}
	})

	t.Run("provider without streamer", func(t *testing.T) {
		c := NewClient(fakeProvider{})
		if _, err := c.StreamSpeech(context.Background(), GenerateSpeechRequest{Text: "hi"}); !errors.Is(err, ErrStreamNotSupported) {
			t.Fatalf("error = %v, want ErrStreamNotSupported", err)
		}
	})

	t.Run("delegates to the streamer", func(t *testing.T) {
		stream := &fakeStream{}
		streamer := &fakeStreamer{stream: stream}
		c := NewClient(streamer)
		got, err := c.StreamSpeech(context.Background(), GenerateSpeechRequest{Text: "hi"})
		if err != nil {
			t.Fatalf("StreamSpeech: %v", err)
		}
		if got != stream || streamer.calls != 1 {
			t.Fatalf("stream = %v, calls = %d, want the streamer's stream and one call", got, streamer.calls)
		}
	})

	t.Run("establishment error is returned", func(t *testing.T) {
		sentinel := errors.New("boom")
		c := NewClient(&fakeStreamer{err: sentinel})
		if _, err := c.StreamSpeech(context.Background(), GenerateSpeechRequest{Text: "hi"}); !errors.Is(err, sentinel) {
			t.Fatalf("error = %v, want the streamer's error", err)
		}
	})
}
