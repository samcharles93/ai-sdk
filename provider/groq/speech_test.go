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
		Model: "canopylabs/orpheus-v1-english",
		Text:  "hello world",
		Voice: "hannah",
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
	if gotBody["model"] != "canopylabs/orpheus-v1-english" || gotBody["input"] != "hello world" || gotBody["voice"] != "hannah" {
		t.Errorf("body = %v, want model/input/voice set", gotBody)
	}
	if gotBody["response_format"] != "wav" {
		t.Errorf("response_format = %v, want default wav", gotBody["response_format"])
	}
	if string(resp.Audio) != "groq-audio-bytes" {
		t.Errorf("audio = %q, want groq-audio-bytes", resp.Audio)
	}
	if resp.Format != "wav" {
		t.Errorf("format = %q, want wav", resp.Format)
	}
}

func TestGenerateSpeech_RequiresVoice(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "canopylabs/orpheus-v1-english",
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
		{name: "missing text", req: speech.GenerateSpeechRequest{Model: "canopylabs/orpheus-v1-english", Voice: "v"}},
		{
			name: "non-wav format rejected",
			req:  speech.GenerateSpeechRequest{Model: "canopylabs/orpheus-v1-english", Text: "hello", Voice: "v", Format: "opus"},
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

func TestGenerateSpeech_FormatPolicy(t *testing.T) {
	// Groq's current Orpheus models are wav-only: wav must reach the wire, and
	// every other format must be rejected before any network call.
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
	ctx := context.Background()
	baseReq := speech.GenerateSpeechRequest{
		Model: "canopylabs/orpheus-v1-english",
		Text:  "hello",
		Voice: "hannah",
	}

	accepted := baseReq
	accepted.Format = "wav"
	resp, err := p.GenerateSpeech(ctx, accepted)
	if err != nil {
		t.Fatalf("GenerateSpeech(wav): %v", err)
	}
	if gotBody["response_format"] != "wav" {
		t.Errorf("response_format = %v, want wav", gotBody["response_format"])
	}
	if resp.Format != "wav" {
		t.Errorf("format = %q, want wav", resp.Format)
	}

	for _, format := range []string{"mp3", "opus", "flac", "mulaw", "ogg"} {
		t.Run("rejects_"+format, func(t *testing.T) {
			req := baseReq
			req.Format = format
			if _, err := p.GenerateSpeech(ctx, req); !errors.Is(err, speech.ErrInvalidRequest) {
				t.Errorf("GenerateSpeech(%s) error = %v, want ErrInvalidRequest", format, err)
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
		Model: "canopylabs/orpheus-v1-english",
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
		Model: "canopylabs/orpheus-v1-english",
		Text:  "hello",
		Voice: "hannah",
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
				Model: "canopylabs/orpheus-v1-english",
				Text:  "hello",
				Voice: "hannah",
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

func TestGenerateSpeech_SampleRate(t *testing.T) {
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

	if _, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model:      "canopylabs/orpheus-v1-english",
		Text:       "hello",
		Voice:      "hannah",
		SampleRate: 24000,
	}); err != nil {
		t.Fatalf("GenerateSpeech: %v", err)
	}
	if gotBody["sample_rate"] != float64(24000) {
		t.Errorf("sample_rate = %v, want 24000", gotBody["sample_rate"])
	}
}

func TestGenerateSpeech_RejectsInstructions(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model:        "canopylabs/orpheus-v1-english",
		Text:         "hello",
		Voice:        "hannah",
		Instructions: "whisper",
	})
	if !errors.Is(err, speech.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestGenerateSpeech_DefaultFormatOverride(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "audio")
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, DefaultFormat: "wav"})
	if _, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "canopylabs/orpheus-v1-english",
		Text:  "hello",
		Voice: "hannah",
	}); err != nil {
		t.Fatalf("GenerateSpeech: %v", err)
	}
	if gotBody["response_format"] != "wav" {
		t.Errorf("response_format = %v, want wav", gotBody["response_format"])
	}

	// An override outside the allowed set is rejected client-side.
	p, _ = New(Config{APIKey: "k", BaseURL: srv.URL, DefaultFormat: "mp3"})
	_, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "canopylabs/orpheus-v1-english",
		Text:  "hello",
		Voice: "hannah",
	})
	if !errors.Is(err, speech.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for an unsupported override, got %v", err)
	}
}

func TestGenerateSpeech_MaxInputChars(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = io.WriteString(w, "audio")
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL, MaxInputChars: 200})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "canopylabs/orpheus-v1-english",
		Text:  strings.Repeat("a", 201),
		Voice: "hannah",
	})
	if !errors.Is(err, speech.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for 201 characters, got %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0 for an oversized input", requests)
	}

	if _, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "canopylabs/orpheus-v1-english",
		Text:  strings.Repeat("a", 200),
		Voice: "hannah",
	}); err != nil {
		t.Fatalf("GenerateSpeech at the limit: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1 at the limit", requests)
	}
}
