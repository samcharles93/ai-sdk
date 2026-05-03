package mistral

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/pkg/chat"
	"github.com/samcharles93/ai-sdk/pkg/embed"
)

func TestNew_RequiresAPIKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error when APIKey is empty")
	}
	if _, err := New(Config{APIKey: "k"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChat_NonStreaming(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"mistral-resp-1",
			"model":"mistral-large",
			"choices":[{"index":0,"message":{"role":"assistant","content":"Bonjour"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "secret", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:       "mistral-large",
		Messages:    []chat.Message{{Role: chat.RoleUser, Content: "hello"}},
		Temperature: 0.7,
		MaxTokens:   128,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path: %s", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("auth: %s", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("content-type: %s", gotCT)
	}
	if gotBody["stream"] != false {
		t.Errorf("stream: %v", gotBody["stream"])
	}
	if gotBody["model"] != "mistral-large" {
		t.Errorf("model: %v", gotBody["model"])
	}
	if _, ok := gotBody["temperature"]; !ok {
		t.Errorf("expected temperature in body")
	}
	if resp.ID != "mistral-resp-1" || resp.Model != "mistral-large" {
		t.Errorf("resp meta: %+v", resp)
	}
	if resp.Content != "Bonjour" || resp.FinishReason != "stop" || resp.Role != chat.RoleAssistant {
		t.Errorf("resp body: %+v", resp)
	}
	if resp.Usage.TotalTokens != 17 {
		t.Errorf("usage: %+v", resp.Usage)
	}
}

func TestChatStream_SSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			t.Errorf("stream flag missing: %v", body["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		writes := []string{
			`data: {"id":"s1","model":"mistral-large","choices":[{"index":0,"delta":{"role":"assistant","content":"He"}}]}` + "\n\n",
			`data: {"id":"s1","model":"mistral-large","choices":[{"index":0,"delta":{"content":"llo"}}]}` + "\n\n",
			`: keepalive` + "\n\n",
			`data: {"id":"s1","model":"mistral-large","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n",
			`data: {"id":"s1","model":"mistral-large","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		}
		for _, s := range writes {
			_, _ = io.WriteString(w, s)
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	st, err := p.ChatStream(context.Background(), chat.Request{
		Model:    "mistral-large",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer st.Close()

	var deltas []string
	var doneChunk *chat.Chunk
	ctx := context.Background()
	for {
		c, err := st.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if c.Done {
			cc := c
			doneChunk = &cc
		} else {
			deltas = append(deltas, c.Delta)
		}
		if c.Done {
			c2, err2 := st.Next(ctx)
			if !errors.Is(err2, io.EOF) {
				t.Fatalf("expected EOF after Done, got chunk=%+v err=%v", c2, err2)
			}
			break
		}
	}
	got := strings.Join(deltas, "")
	if got != "Hello" {
		t.Errorf("deltas concat = %q, want %q", got, "Hello")
	}
	if doneChunk == nil {
		t.Fatal("never saw a Done=true chunk")
	}
	if doneChunk.FinishReason != "stop" {
		t.Errorf("finish_reason = %q", doneChunk.FinishReason)
	}
	if doneChunk.Usage == nil || doneChunk.Usage.TotalTokens != 6 {
		t.Errorf("usage on done chunk: %+v", doneChunk.Usage)
	}
}

func TestEmbed(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"embd-1",
			"object":"list",
			"model":"mistral-embed",
			"data":[
				{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]},
				{"object":"embedding","index":1,"embedding":[0.4,0.5,0.6]}
			],
			"usage":{"prompt_tokens":5,"total_tokens":5}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "secret", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Embed(context.Background(), embed.Request{
		Model:  "mistral-embed",
		Inputs: []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotPath != "/embeddings" {
		t.Errorf("path: %s", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("auth: %s", gotAuth)
	}
	if gotBody["model"] != "mistral-embed" {
		t.Errorf("model: %v", gotBody["model"])
	}
	inputs, ok := gotBody["input"].([]any)
	if !ok || len(inputs) != 2 {
		t.Fatalf("expected 2 inputs in body, got %v", gotBody["input"])
	}
	if resp.Model != "mistral-embed" {
		t.Errorf("resp model: %s", resp.Model)
	}
	if len(resp.Embeddings) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(resp.Embeddings))
	}
	if resp.Embeddings[0].Index != 0 || len(resp.Embeddings[0].Vector) != 3 {
		t.Errorf("embedding[0]: %+v", resp.Embeddings[0])
	}
	if resp.Embeddings[1].Index != 1 || len(resp.Embeddings[1].Vector) != 3 {
		t.Errorf("embedding[1]: %+v", resp.Embeddings[1])
	}
	if resp.Usage.TotalTokens != 5 {
		t.Errorf("usage: %+v", resp.Usage)
	}
}

func TestChat_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid api key"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model:    "mistral-large",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "x"}},
	})
	if !errors.Is(err, chat.ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got %v", err)
	}
}

func TestChat_EmptyModel(t *testing.T) {
	p, _ := New(Config{APIKey: "k", BaseURL: "http://example.invalid"})
	_, err := p.Chat(context.Background(), chat.Request{
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "x"}},
	})
	if !errors.Is(err, chat.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestEmbed_EmptyModel(t *testing.T) {
	p, _ := New(Config{APIKey: "k", BaseURL: "http://example.invalid"})
	_, err := p.Embed(context.Background(), embed.Request{
		Inputs: []string{"x"},
	})
	if !errors.Is(err, chat.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}
