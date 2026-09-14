package groq

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	errx "github.com/samcharles93/ai-sdk/error"
	"github.com/samcharles93/ai-sdk/speech"
)

func TestGenerateSpeech_Success(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "groq-audio-bytes")
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "playai-tts",
		Text:  "hello world",
		Voice: "Fritz-PlayAI",
	})
	if err != nil {
		t.Fatalf("GenerateSpeech: %v", err)
	}
	if gotPath != "/audio/speech" {
		t.Errorf("path = %q, want /audio/speech", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth = %q, want Bearer test-key", gotAuth)
	}
	if gotBody["model"] != "playai-tts" || gotBody["input"] != "hello world" || gotBody["voice"] != "Fritz-PlayAI" {
		t.Errorf("body = %v, want model/input/voice set", gotBody)
	}
	if gotBody["response_format"] != "mp3" {
		t.Errorf("response_format = %v, want default mp3", gotBody["response_format"])
	}
	if string(resp.Audio) != "groq-audio-bytes" {
		t.Errorf("audio = %q, want groq-audio-bytes", resp.Audio)
	}
	if resp.Format != "mp3" {
		t.Errorf("format = %q, want mp3", resp.Format)
	}
}

func TestGenerateSpeech_RequiresVoice(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "playai-tts",
		Text:  "hello",
	})
	if !errors.Is(err, speech.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
	if !strings.Contains(err.Error(), "voice is required") {
		t.Fatalf("error %q should explain the missing voice", err)
	}
}

func TestGenerateSpeech_Validation(t *testing.T) {
	tests := []struct {
		name string
		req  speech.GenerateSpeechRequest
	}{
		{name: "missing model", req: speech.GenerateSpeechRequest{Text: "hello", Voice: "v"}},
		{name: "missing text", req: speech.GenerateSpeechRequest{Model: "playai-tts", Voice: "v"}},
		{
			name: "openai-only format rejected",
			req:  speech.GenerateSpeechRequest{Model: "playai-tts", Text: "hello", Voice: "v", Format: "opus"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = p.GenerateSpeech(context.Background(), tc.req)
			if !errors.Is(err, speech.ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got %v", err)
			}
		})
	}
}

func TestGenerateSpeech_AcceptedFormats(t *testing.T) {
	for _, format := range []string{"mp3", "wav", "flac", "mulaw", "ogg"} {
		t.Run(format, func(t *testing.T) {
			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, "audio")
			}))
			defer srv.Close()
			p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})

			resp, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
				Model:  "playai-tts",
				Text:   "hello",
				Voice:  "Fritz-PlayAI",
				Format: format,
			})
			if err != nil {
				t.Fatalf("GenerateSpeech(%s): %v", format, err)
			}
			if gotBody["response_format"] != format {
				t.Errorf("response_format = %v, want %s", gotBody["response_format"], format)
			}
			if resp.Format != format {
				t.Errorf("format = %q, want %q", resp.Format, format)
			}
		})
	}
}

func TestGenerateSpeech_InvalidVoice_TypedProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid voice"}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})

	_, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "playai-tts",
		Text:  "hello",
		Voice: "not-a-voice",
	})

	var perr *errx.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("expected *errx.ProviderError, got %T: %v", err, err)
	}
	if perr.Provider != "groq" {
		t.Errorf("Provider = %q, want groq", perr.Provider)
	}
	if perr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", perr.StatusCode, http.StatusBadRequest)
	}
	if perr.Retryable {
		t.Error("Retryable = true, want false for 400")
	}
	if !errors.Is(err, speech.ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestGenerateSpeech_RateLimit_TypedProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "4")
		w.Header().Set("X-Request-Id", "req-groq-789")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"slow down"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})

	_, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "playai-tts",
		Text:  "hello",
		Voice: "Fritz-PlayAI",
	})

	var perr *errx.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("expected *errx.ProviderError, got %T: %v", err, err)
	}
	if perr.Provider != "groq" {
		t.Errorf("Provider = %q, want groq", perr.Provider)
	}
	if perr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want %d", perr.StatusCode, http.StatusTooManyRequests)
	}
	if perr.RequestID != "req-groq-789" {
		t.Errorf("RequestID = %q, want req-groq-789", perr.RequestID)
	}
	if perr.RetryAfter != 4*time.Second {
		t.Errorf("RetryAfter = %v, want 4s", perr.RetryAfter)
	}
	if !perr.Retryable {
		t.Error("Retryable = false, want true for 429")
	}
	if !errors.Is(err, speech.ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestGenerateSpeech_AuthAndServerErrors_TypedProviderError(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		want          error
		wantRetryable bool
	}{
		{name: "unauthorised", status: http.StatusUnauthorized, want: speech.ErrAuthFailed},
		{name: "forbidden", status: http.StatusForbidden, want: speech.ErrAuthFailed},
		{name: "server error", status: http.StatusInternalServerError, want: speech.ErrProviderUnavailable, wantRetryable: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"error":"nope"}`)
			}))
			defer srv.Close()
			p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})

			_, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
				Model: "playai-tts",
				Text:  "hello",
				Voice: "Fritz-PlayAI",
			})

			var perr *errx.ProviderError
			if !errors.As(err, &perr) {
				t.Fatalf("expected *errx.ProviderError, got %T: %v", err, err)
			}
			if perr.StatusCode != tc.status {
				t.Errorf("StatusCode = %d, want %d", perr.StatusCode, tc.status)
			}
			if perr.Retryable != tc.wantRetryable {
				t.Errorf("Retryable = %v, want %v", perr.Retryable, tc.wantRetryable)
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("expected %v, got %v", tc.want, err)
			}
		})
	}
}
