package tts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

// newSpeechServer returns an httptest server that decodes the JSON request
// body into capture (when non-nil), then writes body with the given status.
func newSpeechServer(t *testing.T, status int, body string, capture func(map[string]any)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			var reqBody map[string]any
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			capture(reqBody)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testConfig(srv *httptest.Server) Config {
	return Config{
		Provider:       "testprov",
		BaseURL:        srv.URL,
		APIKey:         "test-key",
		HTTPClient:     srv.Client(),
		AllowedFormats: map[string]bool{"mp3": true, "wav": true},
		DefaultVoice:   "default-voice",
		DefaultFormat:  "mp3",
	}
}

func TestGenerate_Success(t *testing.T) {
	var gotPath, gotAuth, gotContentType string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_, _ = io.WriteString(w, "audio-bytes")
	}))
	defer srv.Close()

	got, err := Generate(context.Background(), testConfig(srv), speech.GenerateSpeechRequest{
		Model: "tts-1",
		Text:  "hello world",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if gotPath != "/audio/speech" {
		t.Errorf("path = %q, want /audio/speech", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth = %q, want Bearer test-key", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotContentType)
	}
	if gotBody["model"] != "tts-1" || gotBody["input"] != "hello world" {
		t.Errorf("body model/input = %v/%v, want tts-1/hello world", gotBody["model"], gotBody["input"])
	}
	if gotBody["voice"] != "default-voice" {
		t.Errorf("voice = %v, want default-voice", gotBody["voice"])
	}
	if gotBody["response_format"] != "mp3" {
		t.Errorf("response_format = %v, want mp3", gotBody["response_format"])
	}
	if _, ok := gotBody["speed"]; ok {
		t.Errorf("speed should be omitted when zero, got %v", gotBody["speed"])
	}
	if string(got.Audio) != "audio-bytes" {
		t.Errorf("audio = %q, want audio-bytes", got.Audio)
	}
	if got.Format != "mp3" {
		t.Errorf("format = %q, want mp3", got.Format)
	}
}

func TestGenerate_RequestOverridesDefaults(t *testing.T) {
	var gotBody map[string]any
	srv := newSpeechServer(t, http.StatusOK, "audio", func(body map[string]any) { gotBody = body })

	got, err := Generate(context.Background(), testConfig(srv), speech.GenerateSpeechRequest{
		Model:  "tts-1",
		Text:   "hello",
		Voice:  "explicit-voice",
		Format: "wav",
		Speed:  1.5,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if gotBody["voice"] != "explicit-voice" {
		t.Errorf("voice = %v, want explicit-voice", gotBody["voice"])
	}
	if gotBody["response_format"] != "wav" {
		t.Errorf("response_format = %v, want wav", gotBody["response_format"])
	}
	if gotBody["speed"] != 1.5 {
		t.Errorf("speed = %v, want 1.5", gotBody["speed"])
	}
	if got.Format != "wav" {
		t.Errorf("format = %q, want wav", got.Format)
	}
}

func TestGenerate_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  func(Config) Config
		req  speech.GenerateSpeechRequest
	}{
		{
			name: "missing model",
			req:  speech.GenerateSpeechRequest{Text: "hello"},
		},
		{
			name: "missing text",
			req:  speech.GenerateSpeechRequest{Model: "tts-1"},
		},
		{
			name: "missing voice without default",
			cfg:  func(c Config) Config { c.DefaultVoice = ""; return c },
			req:  speech.GenerateSpeechRequest{Model: "tts-1", Text: "hello"},
		},
		{
			name: "unsupported format",
			req:  speech.GenerateSpeechRequest{Model: "tts-1", Text: "hello", Format: "bogus"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newSpeechServer(t, http.StatusOK, "", nil)
			cfg := testConfig(srv)
			if tc.cfg != nil {
				cfg = tc.cfg(cfg)
			}
			_, err := Generate(context.Background(), cfg, tc.req)
			if !errors.Is(err, speech.ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got %v", err)
			}
		})
	}
}

func TestGenerate_DefaultErrorClassification(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, speech.ErrAuthFailed},
		{http.StatusForbidden, speech.ErrAuthFailed},
		{http.StatusTooManyRequests, speech.ErrRateLimited},
		{http.StatusInternalServerError, speech.ErrProviderUnavailable},
		{http.StatusBadRequest, speech.ErrProviderUnavailable},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := newSpeechServer(t, tc.status, "boom", nil)
			_, err := Generate(context.Background(), testConfig(srv), speech.GenerateSpeechRequest{
				Model: "tts-1",
				Text:  "hello",
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if !strings.Contains(err.Error(), "testprov: status") {
				t.Fatalf("error %q should name the provider and status", err)
			}
		})
	}
}

func TestGenerate_ClassifyErrorOverride(t *testing.T) {
	srv := newSpeechServer(t, http.StatusTeapot, `{"error":"nope"}`, nil)
	cfg := testConfig(srv)
	sentinel := errors.New("custom classification")
	var gotStatus int
	var gotSnippet string
	cfg.ClassifyError = func(resp *http.Response, snippet string) error {
		gotStatus = resp.StatusCode
		gotSnippet = snippet
		return sentinel
	}

	_, err := Generate(context.Background(), cfg, speech.GenerateSpeechRequest{Model: "m", Text: "t"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected custom sentinel, got %v", err)
	}
	if gotStatus != http.StatusTeapot {
		t.Errorf("status = %d, want %d", gotStatus, http.StatusTeapot)
	}
	if !strings.Contains(gotSnippet, "nope") {
		t.Errorf("snippet = %q, want sanitised body", gotSnippet)
	}
}

func TestGenerate_ApplyOptions(t *testing.T) {
	var gotBody map[string]any
	srv := newSpeechServer(t, http.StatusOK, "audio", func(body map[string]any) { gotBody = body })
	cfg := testConfig(srv)
	cfg.ApplyOptions = func(body map[string]any, req speech.GenerateSpeechRequest) {
		body["instructions"] = "speak slowly"
	}

	if _, err := Generate(context.Background(), cfg, speech.GenerateSpeechRequest{Model: "m", Text: "t"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if gotBody["instructions"] != "speak slowly" {
		t.Errorf("instructions = %v, want speak slowly", gotBody["instructions"])
	}
}
