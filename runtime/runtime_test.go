package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
)

// openAICompatibleHandler is a minimal OpenAI-compatible chat completions
// endpoint for testing the runtime end-to-end. It supports both streaming
// and non-streaming requests.
func openAICompatibleHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/chat/completions" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	body, _ := io.ReadAll(r.Body)
	var req struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)

	if !req.Stream {
		resp := map[string]any{
			"id":      "chatcmpl-test",
			"model":   req.Model,
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "hello from " + req.Model}}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	chunks := []map[string]any{
		{"id": "chatcmpl-test", "model": req.Model, "choices": []map[string]any{{"delta": map[string]any{"role": "assistant"}}}},
		{"id": "chatcmpl-test", "model": req.Model, "choices": []map[string]any{{"delta": map[string]any{"content": "hello"}}}},
		{"id": "chatcmpl-test", "model": req.Model, "choices": []map[string]any{{"delta": map[string]any{"content": " from "}}}},
		{"id": "chatcmpl-test", "model": req.Model, "choices": []map[string]any{{"delta": map[string]any{"content": req.Model}, "finish_reason": "stop"}}},
	}
	for _, ch := range chunks {
		data, _ := json.Marshal(ch)
		_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
		flusher.Flush()
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

func TestRuntimeChatWithCustomOpenAICompatibleProvider(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(openAICompatibleHandler))
	defer ts.Close()

	RegisterBuiltinClasses()

	rt := NewRuntime(Config{
		Providers: map[string]ProviderConfig{
			"local": {
				ID:      "local",
				Class:   "openai-compatible",
				BaseURL: ts.URL,
				Auth: AuthConfig{
					Type:   AuthTypeAPIKey,
					APIKey: "test-key",
				},
			},
		},
	})

	result, err := rt.Chat(context.Background(), "local/test-model", core.GenerateOptions{
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hello from test-model" {
		t.Fatalf("text = %q", result.Text)
	}
	if result.TotalUsage.PromptTokens != 10 {
		t.Fatalf("prompt tokens = %d", result.TotalUsage.PromptTokens)
	}
}

func TestRuntimeChatStreamWithCustomProvider(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(openAICompatibleHandler))
	defer ts.Close()

	RegisterBuiltinClasses()

	rt := NewRuntime(Config{
		Providers: map[string]ProviderConfig{
			"local": {
				ID:      "local",
				Class:   "openai-compatible",
				BaseURL: ts.URL,
				Auth: AuthConfig{
					Type:   AuthTypeAPIKey,
					APIKey: "test-key",
				},
			},
		},
	})

	stream, err := rt.ChatStream(context.Background(), "local/test-model", core.GenerateOptions{
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var text strings.Builder
	for p := range stream.FullStream {
		if p.Type == core.StreamPartTextDelta {
			text.WriteString(p.TextDelta)
		}
	}

	got := text.String()
	if got != "hello from test-model" {
		t.Fatalf("streamed text = %q", got)
	}
}

func TestRuntimeDefaultProvider(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(openAICompatibleHandler))
	defer ts.Close()

	RegisterBuiltinClasses()

	rt := NewRuntime(Config{
		DefaultProvider: "local",
		Providers: map[string]ProviderConfig{
			"local": {
				ID:      "local",
				Class:   "openai-compatible",
				BaseURL: ts.URL,
				Auth: AuthConfig{
					Type:   AuthTypeAPIKey,
					APIKey: "test-key",
				},
			},
		},
	})

	ref, err := rt.ParseModelRef("test-model")
	if err != nil {
		t.Fatal(err)
	}
	if ref.ProviderID != "local" || ref.ModelID != "test-model" {
		t.Fatalf("ref = %+v", ref)
	}
}

func TestRuntimeCatalogProviderResolution(t *testing.T) {
	RegisterBuiltinClasses()

	catalog := NewCatalog(CatalogOptions{})
	if err := catalog.LoadFromJSON([]byte(`{
		"groq": {
			"id": "groq",
			"npm": "@ai-sdk/groq",
			"api": "https://api.groq.com/openai/v1",
			"env": ["GROQ_API_KEY"],
			"models": {
				"llama3-8b-8192": {"id": "llama3-8b-8192"}
			}
		}
	}`)); err != nil {
		t.Fatal(err)
	}

	rt := NewRuntimeWithCatalog(Config{}, catalog)

	cfg, err := rt.buildProviderConfig("groq")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Class != "groq" {
		t.Fatalf("class = %q, want groq", cfg.Class)
	}
	if cfg.BaseURL != "https://api.groq.com/openai/v1" {
		t.Fatalf("base_url = %q", cfg.BaseURL)
	}
}

func TestRuntimeMissingProvider(t *testing.T) {
	RegisterBuiltinClasses()
	rt := NewRuntime(Config{})

	_, err := rt.Chat(context.Background(), "missing/model", core.GenerateOptions{})
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("err = %v", err)
	}
}
