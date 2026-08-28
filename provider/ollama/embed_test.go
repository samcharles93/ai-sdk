package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	errx "github.com/samcharles93/ai-sdk/error"

	"github.com/samcharles93/ai-sdk/embed"
)

func TestEmbed_Success(t *testing.T) {
	var got ollamaEmbedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":             "nomic-embed",
			"embeddings":        [][]float32{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}},
			"prompt_eval_count": 7,
		})
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	resp, err := p.Embed(context.Background(), embed.Request{
		Model:  "nomic-embed",
		Inputs: []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if got.Model != "nomic-embed" {
		t.Errorf("request model = %q", got.Model)
	}
	if len(got.Input) != 2 || got.Input[0] != "hello" || got.Input[1] != "world" {
		t.Errorf("request input = %+v", got.Input)
	}
	if resp.Model != "nomic-embed" {
		t.Errorf("response model = %q", resp.Model)
	}
	if len(resp.Embeddings) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(resp.Embeddings))
	}
	if resp.Embeddings[0].Index != 0 || resp.Embeddings[1].Index != 1 {
		t.Errorf("indexes = %d, %d", resp.Embeddings[0].Index, resp.Embeddings[1].Index)
	}
	want0 := []float32{0.1, 0.2, 0.3}
	want1 := []float32{0.4, 0.5, 0.6}
	if !equalVec(resp.Embeddings[0].Vector, want0) {
		t.Errorf("vector[0] = %v want %v", resp.Embeddings[0].Vector, want0)
	}
	if !equalVec(resp.Embeddings[1].Vector, want1) {
		t.Errorf("vector[1] = %v want %v", resp.Embeddings[1].Vector, want1)
	}
	if resp.Usage.PromptTokens != 7 || resp.Usage.TotalTokens != 7 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func equalVec(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestEmbed_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	_, err := p.Embed(context.Background(), embed.Request{
		Model:  "nomic-embed",
		Inputs: []string{"hi"},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, embed.ErrProviderUnavailable) {
		t.Errorf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestEmbed_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	_, err := p.Embed(context.Background(), embed.Request{
		Model:  "nomic-embed",
		Inputs: []string{"hi"},
	})
	if !errors.Is(err, embed.ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestEmbed_RateLimit_TypedProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.Header().Set("X-Request-Id", "req-embed-456")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"slow down"}`)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	_, err := p.Embed(context.Background(), embed.Request{
		Model:  "nomic-embed",
		Inputs: []string{"hi"},
	})

	var perr *errx.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("expected *errx.ProviderError, got %T: %v", err, err)
	}
	if perr.Provider != "ollama" {
		t.Errorf("Provider = %q, want ollama", perr.Provider)
	}
	if perr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want %d", perr.StatusCode, http.StatusTooManyRequests)
	}
	if perr.RequestID != "req-embed-456" {
		t.Errorf("RequestID = %q, want req-embed-456", perr.RequestID)
	}
	if perr.RetryAfter != 3*time.Second {
		t.Errorf("RetryAfter = %v, want 3s", perr.RetryAfter)
	}
	if !perr.Retryable {
		t.Error("Retryable = false, want true for 429")
	}
}

func TestEmbed_EmptyModel(t *testing.T) {
	p := New(Config{})
	_, err := p.Embed(context.Background(), embed.Request{
		Inputs: []string{"hi"},
	})
	if !errors.Is(err, embed.ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestEmbed_NoInputs(t *testing.T) {
	p := New(Config{})
	_, err := p.Embed(context.Background(), embed.Request{
		Model: "nomic-embed",
	})
	if !errors.Is(err, embed.ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestEmbed_LengthMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":             "nomic-embed",
			"embeddings":        [][]float32{{0.1, 0.2}},
			"prompt_eval_count": 3,
		})
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	_, err := p.Embed(context.Background(), embed.Request{
		Model:  "nomic-embed",
		Inputs: []string{"a", "b"},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, embed.ErrProviderUnavailable) {
		t.Errorf("expected wrap of ErrProviderUnavailable, got %v", err)
	}
}
