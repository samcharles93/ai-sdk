package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

// newSpeechServer returns an httptest server that decodes the JSON request
// body into capture (when non-nil) and writes body with the given status.
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
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGenerateSpeech_Defaults(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_, _ = w.Write([]byte("openai-audio"))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "tts-1",
		Text:  "hello",
	})
	if err != nil {
		t.Fatalf("GenerateSpeech: %v", err)
	}
	// New normalises a pathless base URL by appending /v1.
	if gotPath != "/v1/audio/speech" {
		t.Errorf("path = %q, want /v1/audio/speech", gotPath)
	}
	if gotBody["voice"] != "alloy" {
		t.Errorf("voice = %v, want alloy default", gotBody["voice"])
	}
	if gotBody["response_format"] != "mp3" {
		t.Errorf("response_format = %v, want mp3 default", gotBody["response_format"])
	}
	if string(resp.Audio) != "openai-audio" || resp.Format != "mp3" {
		t.Errorf("response = %q/%q, want openai-audio/mp3", resp.Audio, resp.Format)
	}
}

func TestGenerateSpeech_ProviderOptions(t *testing.T) {
	tests := []struct {
		name      string
		req       speech.GenerateSpeechRequest
		wantSpeed any
	}{
		{
			name: "options speed applies when request speed is zero",
			req: speech.GenerateSpeechRequest{
				Model: "tts-1",
				Text:  "hello",
				ProviderOptions: map[string]any{
					"openai": map[string]any{"instructions": "speak slowly", "speed": 2.0},
				},
			},
			wantSpeed: 2.0,
		},
		{
			name: "request speed wins over options speed",
			req: speech.GenerateSpeechRequest{
				Model: "tts-1",
				Text:  "hello",
				Speed: 1.25,
				ProviderOptions: map[string]any{
					"openai": map[string]any{"speed": 2.0},
				},
			},
			wantSpeed: 1.25,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody map[string]any
			srv := newSpeechServer(t, http.StatusOK, "audio", func(body map[string]any) { gotBody = body })
			p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := p.GenerateSpeech(context.Background(), tc.req); err != nil {
				t.Fatalf("GenerateSpeech: %v", err)
			}
			if gotBody["speed"] != tc.wantSpeed {
				t.Errorf("speed = %v, want %v", gotBody["speed"], tc.wantSpeed)
			}
			if tc.wantSpeed == 2.0 && gotBody["instructions"] != "speak slowly" {
				t.Errorf("instructions = %v, want speak slowly", gotBody["instructions"])
			}
		})
	}
}

func TestGenerateSpeech_InvalidFormat(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model:  "tts-1",
		Text:   "hello",
		Format: "bogus",
	})
	if !errors.Is(err, speech.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestGenerateSpeech_ErrorClassification(t *testing.T) {
	srv := newSpeechServer(t, http.StatusUnauthorized, "invalid key", nil)
	p, err := New(Config{APIKey: "bad", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "tts-1",
		Text:  "hello",
	})
	if !errors.Is(err, speech.ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got %v", err)
	}
}

func TestGenerateSpeech_SpeechConfigRequiresVoice(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Speech: &SpeechConfig{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "tts-1",
		Text:  "hello",
	})
	if !errors.Is(err, speech.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestGenerateSpeech_TypedInstructions(t *testing.T) {
	var gotBody map[string]any
	srv := newSpeechServer(t, http.StatusOK, "audio", func(body map[string]any) { gotBody = body })
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model:        "gpt-4o-mini-tts",
		Text:         "hello",
		Instructions: "speak slowly",
	}); err != nil {
		t.Fatalf("GenerateSpeech: %v", err)
	}
	if gotBody["instructions"] != "speak slowly" {
		t.Fatalf("instructions = %v, want speak slowly", gotBody["instructions"])
	}
}

func TestGenerateSpeech_RejectsSampleRate(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model:      "gpt-4o-mini-tts",
		Text:       "hello",
		SampleRate: 24000,
	})
	if !errors.Is(err, speech.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestGenerateSpeech_MaxInputChars(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte("audio"))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL, MaxInputChars: 4096})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "tts-1",
		Text:  strings.Repeat("a", 4097),
	})
	if !errors.Is(err, speech.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for 4097 characters, got %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0 for an oversized input", requests)
	}

	if _, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "tts-1",
		Text:  strings.Repeat("a", 4096),
	}); err != nil {
		t.Fatalf("GenerateSpeech at the limit: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1 at the limit", requests)
	}
}
