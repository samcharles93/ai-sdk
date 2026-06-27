package openai

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
)

func TestNew_RequiresAPIKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error when APIKey is empty")
	}
	if _, err := New(Config{APIKey: "k"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		// Host-only URLs get the conventional /v1 appended.
		"https://api.openai.com":  "https://api.openai.com/v1",
		"https://api.openai.com/": "https://api.openai.com/v1",
		"http://localhost:11434":  "http://localhost:11434/v1",
		// URLs that already carry a path are taken as the complete base.
		"https://api.deepseek.com/v1":                             "https://api.deepseek.com/v1",
		"https://openrouter.ai/api/v1":                            "https://openrouter.ai/api/v1",
		"https://api.groq.com/openai/v1":                          "https://api.groq.com/openai/v1",
		"https://generativelanguage.googleapis.com/v1beta/openai": "https://generativelanguage.googleapis.com/v1beta/openai",
		// Trailing slashes are trimmed before the decision.
		"https://api.deepseek.com/v1/": "https://api.deepseek.com/v1",
	}
	for in, want := range cases {
		if got := normalizeBaseURL(in); got != want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", in, got, want)
		}
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
			"id":"resp-1",
			"model":"gpt-4o",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "secret", BaseURL: srv.URL})
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
	if gotPath != "/v1/chat/completions" {
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
	if gotBody["model"] != "gpt-4o" {
		t.Errorf("model: %v", gotBody["model"])
	}
	if _, ok := gotBody["temperature"]; !ok {
		t.Errorf("expected temperature in body")
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

func TestChat_ToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp-tc",
			"model":"gpt-4o",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":null,
					"tool_calls":[
						{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}
					]
				},
				"finish_reason":"tool_calls"
			}],
			"usage":{"prompt_tokens":50,"completion_tokens":15,"total_tokens":65}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:    "gpt-4o",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "What's the weather in Paris?"}},
		Tools: []chat.Tool{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("tool call id: %s", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("tool call name: %s", tc.Name)
	}
	if tc.Arguments != `{"city":"Paris"}` {
		t.Errorf("tool call arguments: %s", tc.Arguments)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("finish_reason: %s", resp.FinishReason)
	}
}

func TestChatStream_SSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			t.Errorf("stream flag missing: %v", body["stream"])
		}
		so, _ := body["stream_options"].(map[string]any)
		if so == nil || so["include_usage"] != true {
			t.Errorf("stream_options.include_usage missing: %v", body["stream_options"])
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

	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
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

func TestChatStream_ToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		writes := []string{
			`data: {"id":"s2","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_xyz","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}` + "\n\n",
			`data: {"id":"s2","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"Tokyo\"}"}}]}}]}` + "\n\n",
			`data: {"id":"s2","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			`data: {"id":"s2","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":50,"completion_tokens":20,"total_tokens":70}}` + "\n\n",
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
		Model:    "gpt-4o",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "Weather in Tokyo?"}},
		Tools: []chat.Tool{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	var toolCallDeltas []chat.ToolCallDelta
	for {
		c, err := st.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		toolCallDeltas = append(toolCallDeltas, c.ToolCallDeltas...)
		if c.Done {
			c2, err2 := st.Next(ctx)
			if !errors.Is(err2, io.EOF) {
				t.Fatalf("expected EOF after Done, got chunk=%+v err=%v", c2, err2)
			}
			break
		}
	}
	if len(toolCallDeltas) == 0 {
		t.Fatal("expected tool call deltas")
	}
	// First delta should have name, second should have args
	first := toolCallDeltas[0]
	if first.Name != "get_weather" {
		t.Errorf("first delta name: %q", first.Name)
	}
	if first.ID != "call_xyz" {
		t.Errorf("first delta id: %q", first.ID)
	}
	// Reconstruct arguments
	var argsBuilder strings.Builder
	for _, d := range toolCallDeltas {
		argsBuilder.WriteString(d.ArgsDelta)
	}
	got := argsBuilder.String()
	want := `{"city":"Tokyo"}`
	if got != want {
		t.Errorf("reconstructed args = %q, want %q", got, want)
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
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model:    "gpt-4o",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "x"}},
	})
	if !errors.Is(err, chat.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
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

func TestChat_EmptyMessages(t *testing.T) {
	p, _ := New(Config{APIKey: "k", BaseURL: "http://example.invalid"})
	_, err := p.Chat(context.Background(), chat.Request{
		Model: "gpt-4o",
	})
	if !errors.Is(err, chat.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}
