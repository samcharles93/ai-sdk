package ollama

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
)

func TestChat_NonStreaming(t *testing.T) {
	var got ollamaRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":      "llama3",
			"created_at": "now",
			"message": map[string]any{
				"role":    "assistant",
				"content": "hello world",
			},
			"done":              true,
			"done_reason":       "stop",
			"prompt_eval_count": 7,
			"eval_count":        3,
		})
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:       "llama3",
		Messages:    []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
		Temperature: 0.7,
		MaxTokens:   128,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got.Stream {
		t.Errorf("expected stream=false, got true")
	}
	if got.Model != "llama3" {
		t.Errorf("model = %q", got.Model)
	}
	if got.Options == nil || got.Options.Temperature != 0.7 || got.Options.NumPredict != 128 {
		t.Errorf("options not propagated: %+v", got.Options)
	}
	if resp.Content != "hello world" {
		t.Errorf("content = %q", resp.Content)
	}
	if resp.Role != chat.RoleAssistant {
		t.Errorf("role = %q", resp.Role)
	}
	if resp.Model != "llama3" {
		t.Errorf("model = %q", resp.Model)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("finish = %q", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 7 || resp.Usage.CompletionTokens != 3 || resp.Usage.TotalTokens != 10 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestChatStream_NDJSON(t *testing.T) {
	body := strings.Join([]string{
		`{"model":"llama3","message":{"role":"assistant","content":"Hel"},"done":false}`,
		`{"model":"llama3","message":{"role":"assistant","content":"lo, "},"done":false}`,
		`{"model":"llama3","message":{"role":"assistant","content":"world!"},"done":false}`,
		`{"model":"llama3","done":true,"done_reason":"stop","prompt_eval_count":5,"eval_count":4}`,
		"",
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	st, err := p.ChatStream(context.Background(), chat.Request{
		Model:    "llama3",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer st.Close()

	var deltas []string
	var finalChunk chat.Chunk
	for {
		c, err := st.Next(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if c.Done {
			finalChunk = c
			break
		}
		deltas = append(deltas, c.Delta)
		if c.Role != chat.RoleAssistant {
			t.Errorf("role = %q", c.Role)
		}
	}
	if got, want := strings.Join(deltas, ""), "Hello, world!"; got != want {
		t.Errorf("deltas = %q want %q", got, want)
	}
	if !finalChunk.Done {
		t.Fatalf("expected final done chunk")
	}
	if finalChunk.FinishReason != "stop" {
		t.Errorf("finish = %q", finalChunk.FinishReason)
	}
	if finalChunk.Usage == nil ||
		finalChunk.Usage.PromptTokens != 5 ||
		finalChunk.Usage.CompletionTokens != 4 ||
		finalChunk.Usage.TotalTokens != 9 {
		t.Errorf("usage = %+v", finalChunk.Usage)
	}

	if _, err := st.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF after done, got %v", err)
	}
}

func TestChat_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model:    "llama3",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, chat.ErrProviderUnavailable) {
		t.Errorf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestChat_EmptyModel(t *testing.T) {
	p := New(Config{})
	_, err := p.Chat(context.Background(), chat.Request{
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if !errors.Is(err, chat.ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
	if _, err := p.ChatStream(context.Background(), chat.Request{}); !errors.Is(err, chat.ErrInvalidRequest) {
		t.Errorf("ChatStream: expected ErrInvalidRequest, got %v", err)
	}
}
