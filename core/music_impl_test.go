package core

import (
	"context"
	"errors"
	"testing"

	"github.com/samcharles93/ai-sdk/music"
)

// mockMusicProvider implements music.Provider for tests.
type mockMusicProvider struct {
	name string
	fn   func(ctx context.Context, req music.GenerateMusicRequest) (music.GenerateMusicResponse, error)
}

func (m *mockMusicProvider) Name() string { return m.name }
func (m *mockMusicProvider) GenerateMusic(ctx context.Context, req music.GenerateMusicRequest) (music.GenerateMusicResponse, error) {
	return m.fn(ctx, req)
}

func TestGenerateMusic_NoProvider(t *testing.T) {
	_, err := GenerateMusic(context.Background(), nil, music.GenerateMusicRequest{Prompt: "p"})
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestGenerateMusic_ValidRequest(t *testing.T) {
	p := &mockMusicProvider{name: "test", fn: func(ctx context.Context, req music.GenerateMusicRequest) (music.GenerateMusicResponse, error) {
		return music.GenerateMusicResponse{Audio: []byte("audio"), Format: "mp3"}, nil
	}}
	resp, err := GenerateMusic(context.Background(), p, music.GenerateMusicRequest{Model: "m", Prompt: "a track"})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Audio) != "audio" || resp.Format != "mp3" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestGenerateMusic_ProviderError(t *testing.T) {
	wantErr := errors.New("provider error")
	p := &mockMusicProvider{name: "test", fn: func(ctx context.Context, req music.GenerateMusicRequest) (music.GenerateMusicResponse, error) {
		return music.GenerateMusicResponse{}, wantErr
	}}
	_, err := GenerateMusic(context.Background(), p, music.GenerateMusicRequest{Prompt: "p"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped provider error, got %v", err)
	}
}

func TestGenerateMusic_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &mockMusicProvider{name: "test"}
	_, err := GenerateMusic(ctx, p, music.GenerateMusicRequest{Prompt: "p"})
	if !errors.Is(err, ErrAborted) {
		t.Errorf("expected ErrAborted, got %v", err)
	}
}
