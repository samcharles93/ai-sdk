package core

import (
	"context"
	"errors"
	"testing"

	"github.com/samcharles93/ai-sdk/video"
)

type mockVideoProvider struct {
	name string
	fn   func(ctx context.Context, req video.GenerateVideoRequest) (video.GenerateVideoResponse, error)
}

func (m *mockVideoProvider) Name() string { return m.name }
func (m *mockVideoProvider) GenerateVideo(ctx context.Context, req video.GenerateVideoRequest) (video.GenerateVideoResponse, error) {
	return m.fn(ctx, req)
}

func TestGenerateVideo_NoProvider(t *testing.T) {
	_, err := GenerateVideo(context.Background(), nil, video.GenerateVideoRequest{Model: "m"})
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestGenerateVideo_ValidRequest(t *testing.T) {
	p := &mockVideoProvider{name: "test", fn: func(ctx context.Context, req video.GenerateVideoRequest) (video.GenerateVideoResponse, error) {
		return video.GenerateVideoResponse{Videos: []video.VideoResult{{URL: "https://example.com/v.mp4"}}}, nil
	}}
	resp, err := GenerateVideo(context.Background(), p, video.GenerateVideoRequest{Model: "m", Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Videos) != 1 {
		t.Errorf("expected 1 video, got %d", len(resp.Videos))
	}
}

func TestGenerateVideo_ProviderError(t *testing.T) {
	wantErr := errors.New("provider error")
	p := &mockVideoProvider{name: "test", fn: func(ctx context.Context, req video.GenerateVideoRequest) (video.GenerateVideoResponse, error) {
		return video.GenerateVideoResponse{}, wantErr
	}}
	_, err := GenerateVideo(context.Background(), p, video.GenerateVideoRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerateVideo_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &mockVideoProvider{name: "test"}
	_, err := GenerateVideo(ctx, p, video.GenerateVideoRequest{Model: "m"})
	if !errors.Is(err, ErrAborted) {
		t.Errorf("expected ErrAborted, got %v", err)
	}
}
