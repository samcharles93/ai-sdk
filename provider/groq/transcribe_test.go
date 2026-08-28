package groq

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	errx "github.com/samcharles93/ai-sdk/error"
	"github.com/samcharles93/ai-sdk/transcribe"
)

func TestTranscribe_RateLimit_TypedProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.Header().Set("X-Request-Id", "req-groq-456")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"slow down"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Transcribe(context.Background(), transcribe.TranscribeRequest{
		Model: "whisper-large-v3",
		Audio: []byte("fake audio data"),
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
	if perr.RequestID != "req-groq-456" {
		t.Errorf("RequestID = %q, want req-groq-456", perr.RequestID)
	}
	if perr.RetryAfter != 7*time.Second {
		t.Errorf("RetryAfter = %v, want 7s", perr.RetryAfter)
	}
	if !perr.Retryable {
		t.Error("Retryable = false, want true for 429")
	}
}
