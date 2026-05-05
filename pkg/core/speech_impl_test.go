package core

import (
	"context"
	"errors"
	"testing"

	"github.com/samcharles93/ai-sdk/pkg/speech"
)

type mockSpeechProvider struct {
	name string
	fn   func(ctx context.Context, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error)
}

func (m *mockSpeechProvider) Name() string { return m.name }
func (m *mockSpeechProvider) GenerateSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
	return m.fn(ctx, req)
}

func TestGenerateSpeech_NoProvider(t *testing.T) {
	_, err := GenerateSpeech(context.Background(), nil, speech.GenerateSpeechRequest{Model: "m"})
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestGenerateSpeech_ValidRequest(t *testing.T) {
	p := &mockSpeechProvider{name: "test", fn: func(ctx context.Context, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
		return speech.GenerateSpeechResponse{Audio: []byte{0x1, 0x2}}, nil
	}}
	resp, err := GenerateSpeech(context.Background(), p, speech.GenerateSpeechRequest{Model: "m", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Audio) == 0 {
		t.Errorf("expected audio, got none")
	}
}

func TestGenerateSpeech_ProviderError(t *testing.T) {
	wantErr := errors.New("provider error")
	p := &mockSpeechProvider{name: "test", fn: func(ctx context.Context, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
		return speech.GenerateSpeechResponse{}, wantErr
	}}
	_, err := GenerateSpeech(context.Background(), p, speech.GenerateSpeechRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerateSpeech_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &mockSpeechProvider{name: "test"}
	_, err := GenerateSpeech(ctx, p, speech.GenerateSpeechRequest{Model: "m"})
	if !errors.Is(err, ErrAborted) {
		t.Errorf("expected ErrAborted, got %v", err)
	}
}
