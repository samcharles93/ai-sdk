package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

func newTestProvider(t *testing.T, handler http.HandlerFunc) (*Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p, err := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p, srv
}

func TestNew_RequiresAPIKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error for empty APIKey")
	} else if !errors.Is(err, chat.ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got %v", err)
	}
}

func TestChat_NonStreaming(t *testing.T) {
	var gotPath, gotKey string
	var gotBody map[string]any

	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.URL.Query().Get("key")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = io.WriteString(w, `{
            "candidates":[{"content":{"role":"model","parts":[{"text":"hello "},{"text":"world"}]},"finishReason":"STOP"}],
            "usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}
        }`)
	})

	resp, err := p.Chat(context.Background(), chat.Request{
		Model: "gemini-1.5-flash",
		Messages: []chat.Message{
			{Role: chat.RoleSystem, Content: "be terse"},
			{Role: chat.RoleUser, Content: "hi"},
			{Role: chat.RoleAssistant, Content: "yo"},
			{Role: chat.RoleUser, Content: "again"},
		},
		Temperature: 0.7,
		MaxTokens:   1024,
		TopP:        0.9,
		Stop:        []string{"###"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if !strings.Contains(gotPath, ":generateContent") {
		t.Errorf("path missing :generateContent: %q", gotPath)
	}
	if !strings.Contains(gotPath, "/v1beta/models/gemini-1.5-flash") {
		t.Errorf("path missing model segment: %q", gotPath)
	}
	if gotKey != "test-key" {
		t.Errorf("key query param: got %q want %q", gotKey, "test-key")
	}

	si, ok := gotBody["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatalf("missing systemInstruction in body: %v", gotBody)
	}
	parts, _ := si["parts"].([]any)
	if len(parts) == 0 {
		t.Fatalf("systemInstruction.parts empty")
	}
	if got := parts[0].(map[string]any)["text"]; got != "be terse" {
		t.Errorf("system text: got %v", got)
	}

	contents, _ := gotBody["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("contents len: got %d want 3", len(contents))
	}
	roles := []string{}
	for _, c := range contents {
		roles = append(roles, c.(map[string]any)["role"].(string))
	}
	wantRoles := []string{"user", "model", "user"}
	for i, r := range wantRoles {
		if roles[i] != r {
			t.Errorf("role[%d]: got %q want %q", i, roles[i], r)
		}
	}

	gen, ok := gotBody["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("missing generationConfig")
	}
	if gen["maxOutputTokens"].(float64) != 1024 {
		t.Errorf("maxOutputTokens: got %v", gen["maxOutputTokens"])
	}
	if gen["temperature"] == nil {
		t.Errorf("temperature missing")
	}
	if gen["topP"] == nil {
		t.Errorf("topP missing")
	}
	if stops, _ := gen["stopSequences"].([]any); len(stops) != 1 || stops[0] != "###" {
		t.Errorf("stopSequences: got %v", gen["stopSequences"])
	}

	if resp.Content != "hello world" {
		t.Errorf("content: got %q", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("finish: got %q", resp.FinishReason)
	}
	if resp.Role != chat.RoleAssistant {
		t.Errorf("role: got %q", resp.Role)
	}
	if resp.Model != "gemini-1.5-flash" {
		t.Errorf("model: got %q", resp.Model)
	}
	if resp.Usage.PromptTokens != 3 || resp.Usage.CompletionTokens != 2 || resp.Usage.TotalTokens != 5 {
		t.Errorf("usage: %+v", resp.Usage)
	}
}

func TestChatStream_SSE(t *testing.T) {
	chunks := []string{
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hel"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"lo, "}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"world"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"!"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":4,"totalTokenCount":8}}`,
	}

	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			t.Errorf("expected streamGenerateContent in path: %q", r.URL.Path)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Errorf("expected alt=sse, got %q", r.URL.Query().Get("alt"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			if flusher != nil {
				flusher.Flush()
			}
		}
	})

	stream, err := p.ChatStream(context.Background(), chat.Request{
		Model:    "gemini-1.5-flash",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer stream.Close()

	var deltas []string
	var final chat.Chunk
	for {
		ch, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if ch.Done {
			final = ch
			// Continue to confirm io.EOF next.
			continue
		}
		deltas = append(deltas, ch.Delta)
	}

	got := strings.Join(deltas, "")
	// Final chunk's Delta carries the last segment ("!") since finishReason arrived with content.
	got += final.Delta
	if got != "Hello, world!" {
		t.Errorf("assembled stream: got %q", got)
	}
	if !final.Done {
		t.Errorf("final chunk not Done")
	}
	if final.FinishReason != "stop" {
		t.Errorf("final finish: got %q", final.FinishReason)
	}
	if final.Usage == nil || final.Usage.TotalTokens != 8 {
		t.Errorf("final usage: %+v", final.Usage)
	}

	// Subsequent Next calls must return io.EOF.
	if _, err := stream.Next(context.Background()); err != io.EOF {
		t.Errorf("post-done: got %v want io.EOF", err)
	}
}

func TestChat_AuthError(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad creds"}}`)
	})
	_, err := p.Chat(context.Background(), chat.Request{
		Model:    "m",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if !errors.Is(err, chat.ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got %v", err)
	}
}

func TestChat_RateLimit(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `slow down`)
	})
	_, err := p.Chat(context.Background(), chat.Request{
		Model:    "m",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if !errors.Is(err, chat.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestChat_EmptyModel(t *testing.T) {
	p, err := New(Config{APIKey: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Chat(context.Background(), chat.Request{
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if !errors.Is(err, chat.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
	if _, err := p.ChatStream(context.Background(), chat.Request{
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	}); !errors.Is(err, chat.ErrInvalidRequest) {
		t.Fatalf("ChatStream: expected ErrInvalidRequest, got %v", err)
	}
}
