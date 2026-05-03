package azure

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
	"github.com/samcharles93/ai-sdk/pkg/image"
)

func TestNew_RequiresAllFields(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}
	if _, err := New(Config{APIKey: "k"}); err == nil {
		t.Fatal("expected error when Endpoint and Deployment missing")
	}
	if _, err := New(Config{APIKey: "k", Endpoint: "https://e.com"}); err == nil {
		t.Fatal("expected error when Deployment missing")
	}
	if _, err := New(Config{APIKey: "k", Endpoint: "https://e.com", Deployment: "d"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChat_NonStreaming(t *testing.T) {
	var gotURL, gotAuth, gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		gotAuth = r.Header.Get("api-key")
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp-1",
			"model":"gpt-4o",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{
		APIKey:     "secret",
		Endpoint:   srv.URL,
		Deployment: "my-deployment",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:       "gpt-4o",
		Messages:    []chat.Message{{Role: chat.RoleUser, Content: "hello"}},
		Temperature: 0.5,
		MaxTokens:   64,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// Verify Azure-specific URL path
	const wantPath = "/openai/deployments/my-deployment/chat/completions?api-version=2024-02-01"
	if gotURL != wantPath {
		t.Errorf("URL = %q, want %q", gotURL, wantPath)
	}
	// Verify api-key header (not Bearer)
	if gotAuth != "secret" {
		t.Errorf("api-key header = %q, want %q", gotAuth, "secret")
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotBody["stream"] != false {
		t.Errorf("stream = %v", gotBody["stream"])
	}
	if gotBody["model"] != "gpt-4o" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if resp.ID != "resp-1" || resp.Model != "gpt-4o" {
		t.Errorf("resp meta: %+v", resp)
	}
	if resp.Content != "hi there" || resp.FinishReason != "stop" || resp.Role != chat.RoleAssistant {
		t.Errorf("resp body: %+v", resp)
	}
	if resp.Usage.TotalTokens != 10 {
		t.Errorf("usage: %+v", resp.Usage)
	}
}

func TestChatStream_SSE(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		writes := []string{
			`data: {"id":"s1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"He"}}]}` + "\n\n",
			`data: {"id":"s1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"llo"}}]}` + "\n\n",
			`: keepalive` + "\n\n",
			`data: {"id":"s1","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n",
			`data: {"id":"s1","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}` + "\n\n",
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

	p, err := New(Config{APIKey: "k", Endpoint: srv.URL, Deployment: "my-dep"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := p.ChatStream(context.Background(), chat.Request{
		Model:    "gpt-4o",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer st.Close()

	// Verify Azure URL
	const wantPath = "/openai/deployments/my-dep/chat/completions?api-version=2024-02-01"
	if gotURL != wantPath {
		t.Errorf("URL = %q, want %q", gotURL, wantPath)
	}

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
	var gotURL, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		gotAuth = r.Header.Get("api-key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"object":"list",
			"data":[
				{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]},
				{"object":"embedding","index":1,"embedding":[0.4,0.5,0.6]}
			],
			"model":"text-embedding-ada-002",
			"usage":{"prompt_tokens":5,"total_tokens":5}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "k", Endpoint: srv.URL, Deployment: "emb-dep"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Embed(context.Background(), embed.Request{
		Model:  "text-embedding-ada-002",
		Inputs: []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	const wantPath = "/openai/deployments/emb-dep/embeddings?api-version=2024-02-01"
	if gotURL != wantPath {
		t.Errorf("URL = %q, want %q", gotURL, wantPath)
	}
	if gotAuth != "k" {
		t.Errorf("api-key = %q", gotAuth)
	}
	if gotBody["model"] != "text-embedding-ada-002" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if resp.Model != "text-embedding-ada-002" {
		t.Errorf("resp model = %q", resp.Model)
	}
	if len(resp.Embeddings) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(resp.Embeddings))
	}
	if resp.Embeddings[0].Index != 0 || len(resp.Embeddings[0].Vector) != 3 {
		t.Errorf("embedding 0: %+v", resp.Embeddings[0])
	}
	if resp.Embeddings[1].Index != 1 || len(resp.Embeddings[1].Vector) != 3 {
		t.Errorf("embedding 1: %+v", resp.Embeddings[1])
	}
	if resp.Usage.TotalTokens != 5 {
		t.Errorf("usage: %+v", resp.Usage)
	}
}

func TestImage_GenerateImage(t *testing.T) {
	var gotURL, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		gotAuth = r.Header.Get("api-key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"created":1713139200,
			"data":[
				{"url":"https://example.com/img1.png","b64_json":"base64data","media_type":"image/png"},
				{"url":"https://example.com/img2.png"}
			]
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "k", Endpoint: srv.URL, Deployment: "dall-e-3"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{
		Model:  "dall-e-3",
		Prompt: "a cat",
		N:      2,
		Size:   "1024x1024",
	})
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}

	const wantPath = "/openai/deployments/dall-e-3/images/generations?api-version=2024-02-01"
	if gotURL != wantPath {
		t.Errorf("URL = %q, want %q", gotURL, wantPath)
	}
	if gotAuth != "k" {
		t.Errorf("api-key = %q", gotAuth)
	}
	if gotBody["model"] != "dall-e-3" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if gotBody["prompt"] != "a cat" {
		t.Errorf("prompt = %v", gotBody["prompt"])
	}
	if len(resp.Images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(resp.Images))
	}
	if resp.Images[0].URL != "https://example.com/img1.png" {
		t.Errorf("image 0 URL = %q", resp.Images[0].URL)
	}
	if resp.Images[0].Base64 != "base64data" {
		t.Errorf("image 0 Base64 = %q", resp.Images[0].Base64)
	}
	if resp.Images[0].MediaType != "image/png" {
		t.Errorf("image 0 MediaType = %q", resp.Images[0].MediaType)
	}
}

func TestChat_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"access denied"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", Endpoint: srv.URL, Deployment: "d"})
	_, err := p.Chat(context.Background(), chat.Request{
		Model:    "gpt-4o",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "x"}},
	})
	if !errors.Is(err, chat.ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got %v", err)
	}
}

func TestChat_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"slow down"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", Endpoint: srv.URL, Deployment: "d"})
	_, err := p.Chat(context.Background(), chat.Request{
		Model:    "gpt-4o",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "x"}},
	})
	if !errors.Is(err, chat.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestEmbed_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"access denied"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", Endpoint: srv.URL, Deployment: "d"})
	_, err := p.Embed(context.Background(), embed.Request{
		Model:  "text-embedding-ada-002",
		Inputs: []string{"x"},
	})
	if !errors.Is(err, embed.ErrAuthFailed) {
		t.Fatalf("expected embed.ErrAuthFailed, got %v", err)
	}
}

func TestImage_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"forbidden"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", Endpoint: srv.URL, Deployment: "d"})
	_, err := p.GenerateImage(context.Background(), image.GenerateImageRequest{
		Model:  "dall-e-3",
		Prompt: "cat",
	})
	if !errors.Is(err, image.ErrAuthFailed) {
		t.Fatalf("expected image.ErrAuthFailed, got %v", err)
	}
}
