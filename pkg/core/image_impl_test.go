package core

import (
	"context"
	"errors"
	"testing"

	"github.com/samcharles93/ai-sdk/pkg/image"
)

// mockImageProvider implements image.Provider for tests.
type mockImageProvider struct {
	name string
	fn   func(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error)
}

func (m *mockImageProvider) Name() string { return m.name }
func (m *mockImageProvider) GenerateImage(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
	return m.fn(ctx, req)
}

func TestGenerateImage_NoProvider(t *testing.T) {
	_, err := GenerateImage(context.Background(), nil, image.GenerateImageRequest{Model: "m"})
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestGenerateImage_ValidRequest(t *testing.T) {
	p := &mockImageProvider{name: "test", fn: func(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
		return image.GenerateImageResponse{Images: []image.GeneratedImage{{URL: "https://example.com/img.png"}}}, nil
	}}
	resp, err := GenerateImage(context.Background(), p, image.GenerateImageRequest{Model: "m", Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Images) != 1 {
		t.Errorf("expected 1 image, got %d", len(resp.Images))
	}
}

func TestGenerateImage_ProviderError(t *testing.T) {
	wantErr := errors.New("provider error")
	p := &mockImageProvider{name: "test", fn: func(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
		return image.GenerateImageResponse{}, wantErr
	}}
	_, err := GenerateImage(context.Background(), p, image.GenerateImageRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerateImage_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &mockImageProvider{name: "test"}
	_, err := GenerateImage(ctx, p, image.GenerateImageRequest{Model: "m"})
	if !errors.Is(err, ErrAborted) {
		t.Errorf("expected ErrAborted, got %v", err)
	}
}
