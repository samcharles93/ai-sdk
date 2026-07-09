package middleware

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/telemetry"
)

// mockSpan records span operations for test assertions.
type mockSpan struct {
	mu         sync.Mutex
	ended      bool
	attributes map[string]string
	err        error
}

func (s *mockSpan) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended = true
}

func (s *mockSpan) SetAttribute(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attributes[key] = value
}

func (s *mockSpan) RecordError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *mockSpan) Ended() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended
}

func (s *mockSpan) Error() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *mockSpan) Attribute(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attributes[key]
}

// mockTracer implements telemetry.Tracer and records created spans.
type mockTracer struct {
	mu    sync.Mutex
	spans []*mockSpan
}

func (t *mockTracer) Start(ctx context.Context, _ string) (context.Context, telemetry.Span) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := &mockSpan{attributes: make(map[string]string)}
	t.spans = append(t.spans, s)
	return ctx, s
}

func (t *mockTracer) LastSpan() *mockSpan {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.spans) == 0 {
		return nil
	}
	return t.spans[len(t.spans)-1]
}

func (t *mockTracer) Spans() []*mockSpan {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]*mockSpan, len(t.spans))
	copy(result, t.spans)
	return result
}

// stubProvider is a controllable chat.Provider for tests.
type stubProvider struct {
	name     string
	chatFn   func(ctx context.Context, req chat.Request) (chat.Response, error)
	streamFn func(ctx context.Context, req chat.Request) (chat.Stream, error)
}

func (p *stubProvider) Name() string { return p.name }

func (p *stubProvider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	return p.chatFn(ctx, req)
}

func (p *stubProvider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	return p.streamFn(ctx, req)
}

// stubStream is a controllable chat.Stream that returns chunks then either
// the configured error or io.EOF.
type stubStream struct {
	chunks []chat.Chunk
	pos    int
	err    error
}

func (s *stubStream) Next(_ context.Context) (chat.Chunk, error) {
	if s.pos >= len(s.chunks) {
		if s.err != nil {
			return chat.Chunk{}, s.err
		}
		return chat.Chunk{}, io.EOF
	}
	c := s.chunks[s.pos]
	s.pos++
	return c, nil
}

func (s *stubStream) Close() error { return nil }

func TestTelemetryMiddleware_Name(t *testing.T) {
	p := &stubProvider{name: "test-provider"}
	mw := NewTelemetryMiddleware(p, &mockTracer{})
	if got := mw.Name(); got != "test-provider" {
		t.Errorf("Name() = %q, want %q", got, "test-provider")
	}
}

func TestTelemetryMiddleware_Chat_Success(t *testing.T) {
	p := &stubProvider{
		name: "test-provider",
		chatFn: func(_ context.Context, _ chat.Request) (chat.Response, error) {
			return chat.Response{Content: "hello"}, nil
		},
	}
	tracer := &mockTracer{}
	mw := NewTelemetryMiddleware(p, tracer)

	req := chat.Request{
		Model: "test-model",
		Messages: []chat.Message{
			{Role: chat.RoleUser, Content: "hi"},
			{Role: chat.RoleAssistant, Content: "hello"},
		},
		Tools: []chat.Tool{{Name: "search"}},
	}

	resp, err := mw.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("Content = %q, want %q", resp.Content, "hello")
	}

	span := tracer.LastSpan()
	if span == nil {
		t.Fatal("expected a span to be created")
	}
	if !span.Ended() {
		t.Error("span should be ended after Chat")
	}
	if got := span.Attribute("provider.name"); got != "test-provider" {
		t.Errorf("provider.name = %q, want %q", got, "test-provider")
	}
	if got := span.Attribute("model"); got != "test-model" {
		t.Errorf("model = %q, want %q", got, "test-model")
	}
	if got := span.Attribute("messages.count"); got != "2" {
		t.Errorf("messages.count = %q, want %q", got, "2")
	}
	if got := span.Attribute("tools.count"); got != "1" {
		t.Errorf("tools.count = %q, want %q", got, "1")
	}
	if err := span.Error(); err != nil {
		t.Errorf("span should not record an error, got %v", err)
	}
}

func TestTelemetryMiddleware_Chat_Error(t *testing.T) {
	wantErr := errors.New("provider failure")
	p := &stubProvider{
		name: "test-provider",
		chatFn: func(_ context.Context, _ chat.Request) (chat.Response, error) {
			return chat.Response{}, wantErr
		},
	}
	tracer := &mockTracer{}
	mw := NewTelemetryMiddleware(p, tracer)

	_, err := mw.Chat(context.Background(), chat.Request{Model: "m"})
	if !errors.Is(err, wantErr) {
		t.Errorf("expected error %v, got %v", wantErr, err)
	}

	span := tracer.LastSpan()
	if span == nil {
		t.Fatal("expected a span to be created")
	}
	if !span.Ended() {
		t.Error("span should be ended after error")
	}
	if !errors.Is(span.Error(), wantErr) {
		t.Errorf("span error = %v, want %v", span.Error(), wantErr)
	}
}

func TestTelemetryMiddleware_ChatStream_Success(t *testing.T) {
	p := &stubProvider{
		name: "test-provider",
		streamFn: func(_ context.Context, _ chat.Request) (chat.Stream, error) {
			return &stubStream{
				chunks: []chat.Chunk{
					{Delta: "hello"},
					{Delta: " world", Done: true},
				},
			}, nil
		},
	}
	tracer := &mockTracer{}
	mw := NewTelemetryMiddleware(p, tracer)
	req := chat.Request{
		Model:    "stream-model",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	}

	stream, err := mw.ChatStream(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for {
		_, err := stream.Next(context.Background())
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("unexpected stream error: %v", err)
		}
	}

	if err := stream.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	span := tracer.LastSpan()
	if span == nil {
		t.Fatal("expected a span to be created")
	}
	if !span.Ended() {
		t.Error("span should be ended after Close")
	}
	if got := span.Attribute("provider.name"); got != "test-provider" {
		t.Errorf("provider.name = %q, want %q", got, "test-provider")
	}
	if got := span.Attribute("model"); got != "stream-model" {
		t.Errorf("model = %q, want %q", got, "stream-model")
	}
	if got := span.Attribute("messages.count"); got != "1" {
		t.Errorf("messages.count = %q, want %q", got, "1")
	}
}

func TestTelemetryMiddleware_ChatStream_Error(t *testing.T) {
	wantErr := errors.New("stream setup failure")
	p := &stubProvider{
		name: "test-provider",
		streamFn: func(_ context.Context, _ chat.Request) (chat.Stream, error) {
			return nil, wantErr
		},
	}
	tracer := &mockTracer{}
	mw := NewTelemetryMiddleware(p, tracer)

	_, err := mw.ChatStream(context.Background(), chat.Request{Model: "m"})
	if !errors.Is(err, wantErr) {
		t.Errorf("expected error %v, got %v", wantErr, err)
	}

	span := tracer.LastSpan()
	if span == nil {
		t.Fatal("expected a span to be created")
	}
	if !span.Ended() {
		t.Error("span should be ended on stream setup error")
	}
	if !errors.Is(span.Error(), wantErr) {
		t.Errorf("span error = %v, want %v", span.Error(), wantErr)
	}
}

func TestTelemetryMiddleware_ChatStream_NextError(t *testing.T) {
	nextErr := errors.New("mid-stream failure")
	p := &stubProvider{
		name: "test-provider",
		streamFn: func(_ context.Context, _ chat.Request) (chat.Stream, error) {
			return &stubStream{
				chunks: []chat.Chunk{{Delta: "ok"}},
				err:    nextErr,
			}, nil
		},
	}
	tracer := &mockTracer{}
	mw := NewTelemetryMiddleware(p, tracer)

	stream, err := mw.ChatStream(context.Background(), chat.Request{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := stream.Next(context.Background()); err != nil {
		t.Fatalf("first Next should succeed: %v", err)
	}

	_, err = stream.Next(context.Background())
	if !errors.Is(err, nextErr) {
		t.Errorf("expected nextErr, got %v", err)
	}

	stream.Close()

	span := tracer.LastSpan()
	if !span.Ended() {
		t.Error("span should be ended after Close even with mid-stream error")
	}
	if !errors.Is(span.Error(), nextErr) {
		t.Errorf("span error = %v, want %v", span.Error(), nextErr)
	}
}

func TestTelemetryMiddleware_ImplementsProvider(t *testing.T) {
	var _ chat.Provider = NewTelemetryMiddleware(&stubProvider{}, telemetry.NoopTracer{})
}
