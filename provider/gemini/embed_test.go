package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/embed"
)

func TestEmbed_Success(t *testing.T) {
	var gotPath, gotKey string
	var gotBody map[string]any

	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.URL.Query().Get("key")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = io.WriteString(w, `{"embeddings":[{"values":[0.1,0.2,0.3]},{"values":[0.4,0.5,0.6]}]}`)
	})

	resp, err := p.Embed(context.Background(), embed.Request{
		Model:  "text-embedding-004",
		Inputs: []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if !strings.Contains(gotPath, ":batchEmbedContents") {
		t.Errorf("path missing :batchEmbedContents: %q", gotPath)
	}
	if !strings.Contains(gotPath, "/v1beta/models/text-embedding-004") {
		t.Errorf("path missing model segment: %q", gotPath)
	}
	if gotKey != "test-key" {
		t.Errorf("key query param: got %q want %q", gotKey, "test-key")
	}

	reqs, ok := gotBody["requests"].([]any)
	if !ok || len(reqs) != 2 {
		t.Fatalf("requests: got %v", gotBody["requests"])
	}
	for i, want := range []string{"hello", "world"} {
		sub := reqs[i].(map[string]any)
		if sub["model"] != "models/text-embedding-004" {
			t.Errorf("sub[%d].model: got %v", i, sub["model"])
		}
		content, _ := sub["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		if len(parts) != 1 {
			t.Fatalf("sub[%d].parts len: got %d", i, len(parts))
		}
		if got := parts[0].(map[string]any)["text"]; got != want {
			t.Errorf("sub[%d].text: got %v want %q", i, got, want)
		}
	}

	if resp.Model != "text-embedding-004" {
		t.Errorf("response model: got %q", resp.Model)
	}
	if len(resp.Embeddings) != 2 {
		t.Fatalf("embeddings len: got %d", len(resp.Embeddings))
	}
	if resp.Embeddings[0].Index != 0 || resp.Embeddings[1].Index != 1 {
		t.Errorf("indices: %+v", resp.Embeddings)
	}
	if len(resp.Embeddings[0].Vector) != 3 || resp.Embeddings[0].Vector[0] != 0.1 {
		t.Errorf("vector[0]: %+v", resp.Embeddings[0].Vector)
	}
	if resp.Embeddings[1].Vector[2] != 0.6 {
		t.Errorf("vector[1]: %+v", resp.Embeddings[1].Vector)
	}
	if (resp.Usage != embed.Usage{}) {
		t.Errorf("usage should be zero: %+v", resp.Usage)
	}
}

func TestEmbed_HTTPError(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `boom`)
	})
	_, err := p.Embed(context.Background(), embed.Request{
		Model:  "m",
		Inputs: []string{"x"},
	})
	if !errors.Is(err, embed.ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestEmbed_AuthError(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad creds"}}`)
	})
	_, err := p.Embed(context.Background(), embed.Request{
		Model:  "m",
		Inputs: []string{"x"},
	})
	if !errors.Is(err, embed.ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got %v", err)
	}
}

func TestEmbed_EmptyModel(t *testing.T) {
	p, err := New(Config{APIKey: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Embed(context.Background(), embed.Request{
		Inputs: []string{"x"},
	})
	if !errors.Is(err, embed.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestEmbed_NoInputs(t *testing.T) {
	p, err := New(Config{APIKey: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Embed(context.Background(), embed.Request{
		Model: "m",
	})
	if !errors.Is(err, embed.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestEmbed_LengthMismatch(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"embeddings":[{"values":[0.1]}]}`)
	})
	_, err := p.Embed(context.Background(), embed.Request{
		Model:  "m",
		Inputs: []string{"a", "b"},
	})
	if !errors.Is(err, embed.ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestEmbed_ErrorScrubsKey(t *testing.T) {
	const secret = "super-secret-key-12345"
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"hit https://example.com/v1beta/models/m:batchEmbedContents?key=`+secret+` and failed"}`)
	})
	// Override apiKey on the provider so the echoed key in the body is the
	// "leaked" one we want to ensure is scrubbed.
	p.apiKey = secret

	_, err := p.Embed(context.Background(), embed.Request{
		Model:  "m",
		Inputs: []string{"x"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, secret) {
		t.Errorf("error contains raw API key: %q", msg)
	}
	if !strings.Contains(msg, "REDACTED") {
		t.Errorf("error missing REDACTED marker: %q", msg)
	}
}
