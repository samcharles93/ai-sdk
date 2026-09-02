package core

import (
	"context"
	"errors"
	"testing"

	"github.com/samcharles93/ai-sdk/image"
)

// mockEditor implements image.Editor for tests.
type mockEditor struct {
	name string
	fn   func(ctx context.Context, req image.EditImageRequest) (image.EditImageResponse, error)
}

func (m *mockEditor) Name() string { return m.name }
func (m *mockEditor) EditImage(ctx context.Context, req image.EditImageRequest) (image.EditImageResponse, error) {
	return m.fn(ctx, req)
}

func TestGenerateImageEdit_NoProvider(t *testing.T) {
	_, err := GenerateImageEdit(context.Background(), nil, image.EditImageRequest{Model: "m"})
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestGenerateImageEdit_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := &mockEditor{name: "test", fn: func(ctx context.Context, req image.EditImageRequest) (image.EditImageResponse, error) {
		t.Error("editor.EditImage should not be called when context is cancelled")
		return image.EditImageResponse{}, nil
	}}
	_, err := GenerateImageEdit(ctx, e, image.EditImageRequest{Model: "m"})
	if !errors.Is(err, ErrAborted) {
		t.Errorf("expected ErrAborted, got %v", err)
	}
}

func TestGenerateImageEdit_Valid(t *testing.T) {
	e := &mockEditor{name: "test", fn: func(ctx context.Context, req image.EditImageRequest) (image.EditImageResponse, error) {
		return image.EditImageResponse{Images: []image.GeneratedImage{{URL: "https://example.com/edit.png"}}}, nil
	}}
	resp, err := GenerateImageEdit(context.Background(), e, image.EditImageRequest{Model: "m", Prompt: "change"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Images) != 1 {
		t.Errorf("expected 1 image, got %d", len(resp.Images))
	}
}

func TestGenerateImageEdit_ProviderError(t *testing.T) {
	want := errors.New("provider error")
	e := &mockEditor{name: "test", fn: func(ctx context.Context, req image.EditImageRequest) (image.EditImageResponse, error) {
		return image.EditImageResponse{}, want
	}}
	_, err := GenerateImageEdit(context.Background(), e, image.EditImageRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, want) {
		t.Errorf("expected wrapped provider error, got %v", err)
	}
}
