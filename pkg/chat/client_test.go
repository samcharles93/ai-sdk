package chat

import (
	"context"
	"errors"
	"io"
	"testing"
)

type fakeStream struct {
	chunks []Chunk
	i      int
	closed bool
}

func (s *fakeStream) Next(ctx context.Context) (Chunk, error) {
	if s.i >= len(s.chunks) {
		return Chunk{}, io.EOF
	}
	c := s.chunks[s.i]
	s.i++
	return c, nil
}

func (s *fakeStream) Close() error {
	s.closed = true
	return nil
}

type fakeProvider struct {
	lastReq    Request
	resp       Response
	respErr    error
	stream     *fakeStream
	streamErr  error
	chatCalls  int
	streamCall int
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Chat(ctx context.Context, req Request) (Response, error) {
	p.chatCalls++
	p.lastReq = req
	return p.resp, p.respErr
}

func (p *fakeProvider) ChatStream(ctx context.Context, req Request) (Stream, error) {
	p.streamCall++
	p.lastReq = req
	if p.streamErr != nil {
		return nil, p.streamErr
	}
	return p.stream, nil
}

func TestNewClientNilProviderChat(t *testing.T) {
	c := NewClient(nil)
	if _, err := c.Chat(context.Background(), Request{}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("Chat with nil provider: got %v, want ErrNoProvider", err)
	}
	if _, err := c.ChatStream(context.Background(), Request{}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("ChatStream with nil provider: got %v, want ErrNoProvider", err)
	}
}

func TestNilClient(t *testing.T) {
	var c *Client
	if _, err := c.Chat(context.Background(), Request{}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("nil client Chat: got %v, want ErrNoProvider", err)
	}
	if _, err := c.ChatStream(context.Background(), Request{}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("nil client ChatStream: got %v, want ErrNoProvider", err)
	}
}

func TestClientChatForcesStreamFalse(t *testing.T) {
	want := Response{ID: "r1", Content: "hi", Role: RoleAssistant}
	p := &fakeProvider{resp: want}
	c := NewClient(p)

	got, err := c.Chat(context.Background(), Request{Model: "m", Stream: true})
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}
	if got.Content != want.Content || got.FinishReason != want.FinishReason || got.Usage != want.Usage {
		t.Fatalf("Chat: got %+v, want %+v", got, want)
	}
	if p.lastReq.Stream != false {
		t.Fatalf("Chat: forwarded Stream=%v, want false", p.lastReq.Stream)
	}
	if p.lastReq.Model != "m" {
		t.Fatalf("Chat: forwarded Model=%q, want %q", p.lastReq.Model, "m")
	}
}

func TestClientChatPassesThroughError(t *testing.T) {
	sentinel := errors.New("boom")
	p := &fakeProvider{respErr: sentinel}
	c := NewClient(p)

	if _, err := c.Chat(context.Background(), Request{Model: "m"}); !errors.Is(err, sentinel) {
		t.Fatalf("Chat: got %v, want %v", err, sentinel)
	}
}

func TestClientChatStreamForcesStreamTrue(t *testing.T) {
	chunks := []Chunk{
		{Delta: "he", Role: RoleAssistant},
		{Delta: "llo"},
		{Done: true, FinishReason: "stop", Usage: &Usage{TotalTokens: 3}},
	}
	fs := &fakeStream{chunks: chunks}
	p := &fakeProvider{stream: fs}
	c := NewClient(p)

	s, err := c.ChatStream(context.Background(), Request{Model: "m", Stream: false})
	if err != nil {
		t.Fatalf("ChatStream: unexpected error: %v", err)
	}
	if p.lastReq.Stream != true {
		t.Fatalf("ChatStream: forwarded Stream=%v, want true", p.lastReq.Stream)
	}

	for i, want := range chunks {
		got, err := s.Next(context.Background())
		if err != nil {
			t.Fatalf("Next #%d: unexpected error: %v", i, err)
		}
		if got.Delta != want.Delta || got.Done != want.Done || got.FinishReason != want.FinishReason {
			t.Fatalf("Next #%d: got %+v, want %+v", i, got, want)
		}
	}
	if _, err := s.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("Next after exhaustion: got %v, want io.EOF", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !fs.closed {
		t.Fatalf("Close: underlying stream not closed")
	}
}

func TestClientChatStreamErrorPassthrough(t *testing.T) {
	sentinel := errors.New("nope")
	p := &fakeProvider{streamErr: sentinel}
	c := NewClient(p)

	if _, err := c.ChatStream(context.Background(), Request{Model: "m"}); !errors.Is(err, sentinel) {
		t.Fatalf("ChatStream: got %v, want %v", err, sentinel)
	}
}

func TestClientProviderAccessor(t *testing.T) {
	p := &fakeProvider{}
	c := NewClient(p)
	if c.Provider() != p {
		t.Fatalf("Provider() did not return underlying provider")
	}
	var nilC *Client
	if nilC.Provider() != nil {
		t.Fatalf("nil client Provider() should be nil")
	}
}
