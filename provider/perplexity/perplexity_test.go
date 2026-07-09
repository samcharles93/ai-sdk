package perplexity

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

func TestNew_RequiresAPIKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error when APIKey is empty")
	}
	if _, err := New(Config{APIKey: "k"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChat_NonStreamingWithCitations(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp-1",
			"model":"sonar-pro",
			"choices":[{"index":0,"message":{"role":"assistant","content":"The answer is 42."},"finish_reason":"stop"}],
			"citations":["https://example.com/ref1","https://example.com/ref2"],
			"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "secret", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:       "sonar-pro",
		Messages:    []chat.Message{{Role: chat.RoleUser, Content: "What is the answer?"}},
		Temperature: 0.5,
		MaxTokens:   64,
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
	if gotBody["model"] != "sonar-pro" {
		t.Errorf("model: %v", gotBody["model"])
	}
	if resp.ID != "resp-1" || resp.Model != "sonar-pro" {
		t.Errorf("resp meta: %+v", resp)
	}
	if resp.Content != "The answer is 42." || resp.FinishReason != "stop" || resp.Role != chat.RoleAssistant {
		t.Errorf("resp body: %+v", resp)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("usage: %+v", resp.Usage)
	}
	// Verify citations in ProviderMetadata.
	if resp.ProviderMetadata == nil {
		t.Fatal("expected ProviderMetadata to be set")
	}
	citations, ok := resp.ProviderMetadata["perplexity:citations"].([]string)
	if !ok {
		t.Fatalf("expected perplexity:citations to be []string, got %T", resp.ProviderMetadata["perplexity:citations"])
	}
	if len(citations) != 2 {
		t.Fatalf("expected 2 citations, got %d", len(citations))
	}
	if citations[0] != "https://example.com/ref1" || citations[1] != "https://example.com/ref2" {
		t.Errorf("citations: %v", citations)
	}
}

func TestChatStream(t *testing.T) {
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
			`data: {"id":"s1","model":"sonar-pro","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}` + "\n\n",
			`data: {"id":"s1","model":"sonar-pro","choices":[{"index":0,"delta":{"content":" world"}}]}` + "\n\n",
			`: keepalive` + "\n\n",
			`data: {"id":"s1","model":"sonar-pro","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n",
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
		Model:    "sonar-pro",
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
	if got != "Hello world" {
		t.Errorf("deltas concat = %q, want %q", got, "Hello world")
	}
	if doneChunk == nil {
		t.Fatal("never saw a Done=true chunk")
	}
	if doneChunk.FinishReason != "stop" {
		t.Errorf("finish_reason = %q", doneChunk.FinishReason)
	}
}

func TestChat_SearchRecencyFilter(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp-sr",
			"model":"sonar-pro",
			"choices":[{"index":0,"message":{"role":"assistant","content":"Search result."},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Chat(context.Background(), chat.Request{
		Model:    "sonar-pro",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "latest news"}},
		Metadata: map[string]string{
			"search_recency_filter": "month",
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotBody["search_recency_filter"] != "month" {
		t.Errorf("search_recency_filter = %v, want 'month'", gotBody["search_recency_filter"])
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
		Model:    "sonar-pro",
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
