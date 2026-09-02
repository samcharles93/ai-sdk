package xai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	errx "github.com/samcharles93/ai-sdk/error"
	"github.com/samcharles93/ai-sdk/image"
)

func TestEditImage_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Errorf("path = %q, want /v1/images/edits", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data") {
			t.Errorf("Content-Type = %q, want multipart/form-data prefix", ct)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		if got := r.FormValue("model"); got != "grok-image" {
			t.Errorf("model = %q, want grok-image", got)
		}
		if got := r.FormValue("prompt"); got != "make it sunset" {
			t.Errorf("prompt = %q, want make it sunset", got)
		}
		files := r.MultipartForm.File["image"]
		if len(files) != 1 {
			t.Fatalf("expected 1 image file part, got %d", len(files))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"url":"https://example.com/out.png","b64_json":"aGVsbG8="}]}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	resp, err := p.EditImage(context.Background(), image.EditImageRequest{
		Model:  "grok-image",
		Prompt: "make it sunset",
		Image:  image.EditImageSource{Data: []byte("fakepngdata"), MediaType: "image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(resp.Images))
	}
	if resp.Images[0].URL != "https://example.com/out.png" {
		t.Errorf("URL = %q, want https://example.com/out.png", resp.Images[0].URL)
	}
	if resp.Images[0].Base64 != "aGVsbG8=" {
		t.Errorf("Base64 = %q, want aGVsbG8=", resp.Images[0].Base64)
	}
}

func TestEditImage_ErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       string
		retryAfter string
		wantErr    error
		wantRetry  bool
	}{
		{"auth", http.StatusUnauthorized, `{"error":"bad key"}`, "", image.ErrAuthFailed, false},
		{"rate_limit", http.StatusTooManyRequests, `{"error":"slow down"}`, "3", image.ErrRateLimited, true},
		{"server_error", http.StatusInternalServerError, `{"error":"boom"}`, "", image.ErrProviderUnavailable, true},
		{"content_filtered", http.StatusBadRequest, `{"error":{"message":"content_filter triggered"}}`, "", image.ErrContentFiltered, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.Header().Set("X-Request-Id", "req-xai-edit-1")
				w.WriteHeader(tc.statusCode)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
			_, err := p.EditImage(context.Background(), image.EditImageRequest{
				Model:  "m",
				Prompt: "x",
				Image:  image.EditImageSource{Data: []byte("data")},
			})

			if !errors.Is(err, tc.wantErr) {
				t.Errorf("errors.Is(%v, %v) = false, want true", err, tc.wantErr)
			}
			var perr *errx.ProviderError
			if !errors.As(err, &perr) {
				t.Fatalf("expected *errx.ProviderError, got %T: %v", err, err)
			}
			if perr.Provider != "xai" {
				t.Errorf("Provider = %q, want xai", perr.Provider)
			}
			if perr.StatusCode != tc.statusCode {
				t.Errorf("StatusCode = %d, want %d", perr.StatusCode, tc.statusCode)
			}
			if perr.RequestID != "req-xai-edit-1" {
				t.Errorf("RequestID = %q, want req-xai-edit-1", perr.RequestID)
			}
			if perr.Retryable != tc.wantRetry {
				t.Errorf("Retryable = %v, want %v", perr.Retryable, tc.wantRetry)
			}
			if tc.retryAfter != "" && perr.RetryAfter != 3*time.Second {
				t.Errorf("RetryAfter = %v, want 3s", perr.RetryAfter)
			}
		})
	}
}

func TestEditImage_Validation(t *testing.T) {
	p, _ := New(Config{APIKey: "k", BaseURL: "https://api.x.ai"})

	// missing model
	_, err := p.EditImage(context.Background(), image.EditImageRequest{Prompt: "x", Image: image.EditImageSource{Data: []byte("d")}})
	if !errors.Is(err, image.ErrInvalidRequest) {
		t.Errorf("missing model: expected ErrInvalidRequest, got %v", err)
	}

	// missing prompt
	_, err = p.EditImage(context.Background(), image.EditImageRequest{Model: "m", Image: image.EditImageSource{Data: []byte("d")}})
	if !errors.Is(err, image.ErrInvalidRequest) {
		t.Errorf("missing prompt: expected ErrInvalidRequest, got %v", err)
	}

	// missing source image
	_, err = p.EditImage(context.Background(), image.EditImageRequest{Model: "m", Prompt: "x"})
	if !errors.Is(err, image.ErrInvalidRequest) {
		t.Errorf("missing image: expected ErrInvalidRequest, got %v", err)
	}
}
