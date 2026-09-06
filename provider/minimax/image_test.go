package minimax

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/image"
)

// pngBytes is a minimal valid PNG header so DetectMediaType identifies it.
var pngBytes = []byte("\x89PNG\r\n\x1a\n00000000000000000000")

func TestGenerateImage_Success(t *testing.T) {
	wantB64 := base64.StdEncoding.EncodeToString(pngBytes)

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":0,"status_msg":"success"},"data":{"image_base64":"`+wantB64+`"}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "test-key", BaseURL: srv.URL})

	resp, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{
		Model:       "image-01",
		Prompt:      "a red fox in the snow",
		AspectRatio: "16:9",
	})
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if len(resp.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(resp.Images))
	}
	if string(resp.Images[0].Data) != string(pngBytes) {
		t.Error("decoded data mismatch")
	}
	if resp.Images[0].MediaType != "image/png" {
		t.Errorf("media type = %q, want image/png", resp.Images[0].MediaType)
	}
	if gotBody["model"] != "image-01" || gotBody["aspect_ratio"] != "16:9" {
		t.Errorf("body = %v, want model image-01 and aspect_ratio 16:9", gotBody)
	}
}

func TestGenerateImage_MissingPrompt(t *testing.T) {
	p := newTestClient(t, &fakeAPI{t: t})
	_, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{})
	if !errors.Is(err, image.ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestGenerateImage_BaseRespError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":1002,"status_msg":"bad prompt"}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "bad prompt") {
		t.Errorf("expected 'bad prompt' error, got %v", err)
	}
}

func TestGenerateImage_PassesNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"type":"auth_error","message":"invalid API key"}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{Prompt: "x"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, image.ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid API key") {
		t.Errorf("expected platform message, got %v", err)
	}
}
