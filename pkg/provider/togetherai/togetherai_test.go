package togetherai

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

	"github.com/samcharles93/ai-sdk/pkg/image"
)

func newTestServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *Provider) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p := New(Config{APIKey: "sk-test", BaseURL: srv.URL})
	return srv, p
}

func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode body: %v (%s)", err, string(b))
	}
	return m
}

func b64png(t *testing.T) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n"))
}

func TestGenerateImage_Success(t *testing.T) {
	want := b64png(t)
	var capturedBody map[string]any
	var capturedAuth string

	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %s", r.Method)
		}
		if r.URL.Path != "/images/generations" {
			t.Errorf("path: %s", r.URL.Path)
		}
		capturedAuth = r.Header.Get("Authorization")
		capturedBody = decodeBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"b64_json": want}},
		})
	})

	resp, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{
		Model:  "black-forest-labs/FLUX.1-schnell-Free",
		Prompt: "a red cube",
		Size:   "1024x768",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if capturedAuth != "Bearer sk-test" {
		t.Fatalf("auth: %q", capturedAuth)
	}
	if capturedBody["model"] != "black-forest-labs/FLUX.1-schnell-Free" ||
		capturedBody["prompt"] != "a red cube" ||
		capturedBody["response_format"] != "base64" {
		t.Fatalf("body: %+v", capturedBody)
	}
	if w, ok := capturedBody["width"].(float64); !ok || int(w) != 1024 {
		t.Fatalf("width: %+v", capturedBody["width"])
	}
	if h, ok := capturedBody["height"].(float64); !ok || int(h) != 768 {
		t.Fatalf("height: %+v", capturedBody["height"])
	}
	if _, ok := capturedBody["n"]; ok {
		t.Fatalf("n should be omitted when 1: %+v", capturedBody)
	}

	if len(resp.Images) != 1 {
		t.Fatalf("images: %d", len(resp.Images))
	}
	if resp.Images[0].Base64 != want {
		t.Fatalf("base64 mismatch")
	}
	if resp.Images[0].MediaType != "image/png" {
		t.Fatalf("media type: %q", resp.Images[0].MediaType)
	}
}

func TestGenerateImage_NMultiple(t *testing.T) {
	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(t, r)
		if n, _ := body["n"].(float64); int(n) != 3 {
			t.Errorf("n: %+v", body["n"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"b64_json": "a"}, {"b64_json": "b"}, {"b64_json": "c"},
			},
		})
	})
	resp, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{
		Model: "m", Prompt: "p", N: 3,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(resp.Images) != 3 {
		t.Fatalf("images: %d", len(resp.Images))
	}
}

func TestGenerateImage_ProviderOptionsTyped(t *testing.T) {
	var captured map[string]any
	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		captured = decodeBody(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"b64_json": "x"}}})
	})
	seed := int64(42)
	_, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{
		Model:  "m",
		Prompt: "p",
		Seed:   &seed,
		ProviderOptions: map[string]any{
			"togetherai": Options{
				Steps:                28,
				Guidance:             3.5,
				DisableSafetyChecker: true,
				Extra:                map[string]any{"strength": 0.85},
			},
			"openai": map[string]any{"ignored": true},
		},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s, _ := captured["steps"].(float64); int(s) != 28 {
		t.Fatalf("steps: %+v", captured["steps"])
	}
	if g, _ := captured["guidance"].(float64); g != 3.5 {
		t.Fatalf("guidance: %+v", captured["guidance"])
	}
	if dsc, _ := captured["disable_safety_checker"].(bool); !dsc {
		t.Fatalf("disable_safety_checker: %+v", captured["disable_safety_checker"])
	}
	if seedv, _ := captured["seed"].(float64); int64(seedv) != 42 {
		t.Fatalf("seed: %+v", captured["seed"])
	}
	if str, _ := captured["strength"].(float64); str != 0.85 {
		t.Fatalf("extra strength passthrough: %+v", captured["strength"])
	}
	if _, exists := captured["ignored"]; exists {
		t.Fatalf("foreign provider option leaked: %+v", captured)
	}
}

func TestGenerateImage_ProviderOptionsAsMap(t *testing.T) {
	var captured map[string]any
	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		captured = decodeBody(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"b64_json": "x"}}})
	})
	_, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{
		Model: "m", Prompt: "p",
		ProviderOptions: map[string]any{
			"togetherai": map[string]any{"steps": 12, "negative_prompt": "blurry"},
		},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s, _ := captured["steps"].(float64); int(s) != 12 {
		t.Fatalf("steps: %+v", captured["steps"])
	}
	if captured["negative_prompt"] != "blurry" {
		t.Fatalf("negative_prompt: %+v", captured["negative_prompt"])
	}
}

func TestGenerateImage_NegativePromptTopLevel(t *testing.T) {
	var captured map[string]any
	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		captured = decodeBody(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"b64_json": "x"}}})
	})
	_, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{
		Model: "m", Prompt: "p", NegativePrompt: "ugly",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if captured["negative_prompt"] != "ugly" {
		t.Fatalf("negative_prompt: %+v", captured["negative_prompt"])
	}
}

func TestGenerateImage_AspectRatioWarn(t *testing.T) {
	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(t, r)
		if _, ok := body["aspect_ratio"]; ok {
			t.Errorf("aspect_ratio should not be sent on the wire")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"b64_json": "x"}}})
	})
	resp, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{
		Model: "m", Prompt: "p", AspectRatio: "16:9",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(resp.Warnings) == 0 || !strings.Contains(strings.ToLower(resp.Warnings[0]), "aspect_ratio") {
		t.Fatalf("warnings: %+v", resp.Warnings)
	}
}

func TestGenerateImage_InvalidSize(t *testing.T) {
	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server must not be called when validation fails")
	})
	_, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{
		Model: "m", Prompt: "p", Size: "huge",
	})
	if !errors.Is(err, image.ErrInvalidRequest) {
		t.Fatalf("err: %v", err)
	}
}

func TestGenerateImage_MissingFields(t *testing.T) {
	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server must not be called")
	})
	if _, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{Prompt: "p"}); !errors.Is(err, image.ErrInvalidRequest) {
		t.Fatalf("missing model: %v", err)
	}
	if _, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{Model: "m"}); !errors.Is(err, image.ErrInvalidRequest) {
		t.Fatalf("missing prompt: %v", err)
	}
}

func TestGenerateImage_HTTPErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"unauth", http.StatusUnauthorized, `{"error":{"message":"bad key"}}`, image.ErrAuthFailed},
		{"forbidden", http.StatusForbidden, `{"error":{"message":"nope"}}`, image.ErrAuthFailed},
		{"rate", http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`, image.ErrRateLimited},
		{"server", http.StatusBadGateway, `{"error":{"message":"oops"}}`, image.ErrProviderUnavailable},
		{"badreq", http.StatusBadRequest, `{"error":{"message":"unknown field"}}`, image.ErrInvalidRequest},
		{"safety", http.StatusBadRequest, `{"error":{"message":"NSFW content blocked by safety checker"}}`, image.ErrContentFiltered},
		{"unparseable", http.StatusInternalServerError, `not json`, image.ErrProviderUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{Model: "m", Prompt: "p"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGenerateImage_EmptyData(t *testing.T) {
	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	})
	_, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{Model: "m", Prompt: "p"})
	if !errors.Is(err, image.ErrProviderUnavailable) {
		t.Fatalf("err: %v", err)
	}
}

func TestProviderName(t *testing.T) {
	p := New(Config{})
	if p.Name() != "togetherai" {
		t.Fatalf("name: %s", p.Name())
	}
}

func TestProviderDefaultBaseURL(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p.baseURL != strings.TrimRight(DefaultBaseURL, "/") {
		t.Fatalf("baseURL: %s", p.baseURL)
	}
}
