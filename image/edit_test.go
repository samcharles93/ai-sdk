package image

import (
	"context"
	"errors"
	"testing"
)

// mockEditorProvider implements both Provider and Editor for tests.
type mockEditorProvider struct {
	name string
	gen  func(ctx context.Context, req GenerateImageRequest) (GenerateImageResponse, error)
	edit func(ctx context.Context, req EditImageRequest) (EditImageResponse, error)
}

func (m *mockEditorProvider) Name() string { return m.name }

func (m *mockEditorProvider) GenerateImage(ctx context.Context, req GenerateImageRequest) (GenerateImageResponse, error) {
	if m.gen == nil {
		return GenerateImageResponse{}, ErrNoProvider
	}
	return m.gen(ctx, req)
}

func (m *mockEditorProvider) EditImage(ctx context.Context, req EditImageRequest) (EditImageResponse, error) {
	if m.edit == nil {
		return EditImageResponse{}, ErrNoProvider
	}
	return m.edit(ctx, req)
}

// genOnlyProvider implements Provider but NOT Editor.
type genOnlyProvider struct{ name string }

func (g *genOnlyProvider) Name() string { return g.name }
func (g *genOnlyProvider) GenerateImage(ctx context.Context, req GenerateImageRequest) (GenerateImageResponse, error) {
	return GenerateImageResponse{}, nil
}

func TestClientEditImage_NilClient(t *testing.T) {
	var c *Client
	_, err := c.EditImage(context.Background(), EditImageRequest{Model: "m", Prompt: "p", Image: EditImageSource{Data: []byte("d")}})
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestClientEditImage_NilProvider(t *testing.T) {
	c := NewClient(nil)
	_, err := c.EditImage(context.Background(), EditImageRequest{Model: "m", Prompt: "p", Image: EditImageSource{Data: []byte("d")}})
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestClientEditImage_NotEditor(t *testing.T) {
	c := NewClient(&genOnlyProvider{name: "gen"})
	_, err := c.EditImage(context.Background(), EditImageRequest{Model: "m", Prompt: "p", Image: EditImageSource{Data: []byte("d")}})
	if !errors.Is(err, ErrEditNotSupported) {
		t.Errorf("expected ErrEditNotSupported, got %v", err)
	}
}

func TestClientEditImage_MissingPrompt(t *testing.T) {
	c := NewClient(&mockEditorProvider{name: "test"})
	_, err := c.EditImage(context.Background(), EditImageRequest{Model: "m", Image: EditImageSource{Data: []byte("d")}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestClientEditImage_Valid(t *testing.T) {
	c := NewClient(&mockEditorProvider{
		name: "test",
		edit: func(ctx context.Context, req EditImageRequest) (EditImageResponse, error) {
			return EditImageResponse{Images: []GeneratedImage{{URL: "https://example.com/out.png"}}}, nil
		},
	})
	resp, err := c.EditImage(context.Background(), EditImageRequest{Model: "m", Prompt: "make it red", Image: EditImageSource{Data: []byte("d")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Images) != 1 {
		t.Errorf("expected 1 image, got %d", len(resp.Images))
	}
	if resp.Images[0].URL != "https://example.com/out.png" {
		t.Errorf("URL = %q", resp.Images[0].URL)
	}
}
