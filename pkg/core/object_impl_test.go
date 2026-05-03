package core

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/samcharles93/ai-sdk/pkg/object"
)

// fakeObjectProvider is a scripted object.Provider for tests.
type fakeObjectProvider struct {
	name string

	streamRes  object.ObjectStream
	streamErr  error
	streamCalls []object.Request
}

func (f *fakeObjectProvider) Name() string {
	if f.name == "" {
		return "fake-object"
	}
	return f.name
}

func (f *fakeObjectProvider) GenerateObject(_ context.Context, _ object.Request) (object.ObjectResult, error) {
	return nil, errors.New("fakeObjectProvider: GenerateObject not scripted")
}

func (f *fakeObjectProvider) StreamObject(_ context.Context, req object.Request) (object.ObjectStream, error) {
	f.streamCalls = append(f.streamCalls, req)
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	return f.streamRes, nil
}

// fakeObjectStream implements object.ObjectStream with canned chunks.
type fakeObjectStream struct {
	chunks []object.ObjectChunk
	idx    int
	closed bool
}

func (s *fakeObjectStream) Next(_ context.Context) (object.ObjectChunk, error) {
	if s.idx >= len(s.chunks) {
		return object.ObjectChunk{}, io.EOF
	}
	c := s.chunks[s.idx]
	s.idx++
	return c, nil
}

func (s *fakeObjectStream) Close() error {
	s.closed = true
	return nil
}

func TestStreamObject_NoProvider(t *testing.T) {
	_, err := StreamObject(context.Background(), nil, object.Request{})
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("want ErrNoProvider, got %v", err)
	}
}

func TestStreamObject_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := &fakeObjectProvider{
		streamRes: &fakeObjectStream{},
	}

	_, err := StreamObject(ctx, p, object.Request{Model: "test"})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("expected ErrAborted, got %v", err)
	}
}

func TestStreamObject_DelegatesToProvider(t *testing.T) {
	chunks := []object.ObjectChunk{
		{Delta: `{"name":"`, Done: false},
		{Delta: `test"}`, Done: true},
	}
	p := &fakeObjectProvider{
		streamRes: &fakeObjectStream{chunks: chunks},
	}

	stream, err := StreamObject(context.Background(), p, object.Request{Model: "test-model", Prompt: "hello"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer stream.Close()

	var deltas []string
	for {
		chunk, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		deltas = append(deltas, chunk.Delta)
	}

	if len(deltas) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %v", len(deltas), deltas)
	}
	if deltas[0] != `{"name":"` || deltas[1] != `test"}` {
		t.Fatalf("unexpected deltas: %v", deltas)
	}

	if len(p.streamCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(p.streamCalls))
	}
	if p.streamCalls[0].Model != "test-model" || p.streamCalls[0].Prompt != "hello" {
		t.Fatalf("unexpected request: %+v", p.streamCalls[0])
	}
}

func TestStreamObject_ProviderError(t *testing.T) {
	provErr := errors.New("provider failure")
	p := &fakeObjectProvider{
		streamErr: provErr,
	}

	_, err := StreamObject(context.Background(), p, object.Request{Model: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, provErr) {
		t.Fatalf("expected provider error, got %v", err)
	}
}
