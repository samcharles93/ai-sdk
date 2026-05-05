package core

import (
	"context"
	"errors"
	"testing"

	"github.com/samcharles93/ai-sdk/pkg/transcribe"
)

type mockTranscribeProvider struct {
	name string
	fn   func(ctx context.Context, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error)
}

func (m *mockTranscribeProvider) Name() string { return m.name }
func (m *mockTranscribeProvider) Transcribe(ctx context.Context, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error) {
	return m.fn(ctx, req)
}

func TestTranscribe_NoProvider(t *testing.T) {
	_, err := Transcribe(context.Background(), nil, transcribe.TranscribeRequest{Model: "m"})
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestTranscribe_ValidRequest(t *testing.T) {
	p := &mockTranscribeProvider{name: "test", fn: func(ctx context.Context, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error) {
		return transcribe.TranscribeResponse{Text: "hello"}, nil
	}}
	resp, err := Transcribe(context.Background(), p, transcribe.TranscribeRequest{Model: "m", Audio: []byte{0x1}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello" {
		t.Errorf("expected hello, got %q", resp.Text)
	}
}

func TestTranscribe_ProviderError(t *testing.T) {
	wantErr := errors.New("provider error")
	p := &mockTranscribeProvider{name: "test", fn: func(ctx context.Context, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error) {
		return transcribe.TranscribeResponse{}, wantErr
	}}
	_, err := Transcribe(context.Background(), p, transcribe.TranscribeRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTranscribe_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &mockTranscribeProvider{name: "test"}
	_, err := Transcribe(ctx, p, transcribe.TranscribeRequest{Model: "m"})
	if !errors.Is(err, ErrAborted) {
		t.Errorf("expected ErrAborted, got %v", err)
	}
}
