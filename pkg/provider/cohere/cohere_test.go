package cohere

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/pkg/chat"
	"github.com/samcharles93/ai-sdk/pkg/embed"
	"github.com/samcharles93/ai-sdk/pkg/rerank"
)

func TestName(t *testing.T) {
	p, err := New(Config{APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "cohere" {
		t.Errorf("Name() = %q, want %q", p.Name(), "cohere")
	}
}

func TestNew_RequiresAPIKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error when APIKey is empty")
	}
	if _, err := New(Config{APIKey: "k"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestChat_NonStreaming tests a basic chat completion without tools.
func TestChat_NonStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat" {
			t.Errorf("path = %s, want /chat", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("auth = %s, want Bearer secret", r.Header.Get("Authorization"))
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "command-r" {
			t.Errorf("model = %v", body["model"])
		}
		if body["message"] != "hello" {
			t.Errorf("message = %v", body["message"])
		}
		if body["stream"] != false {
			t.Errorf("stream = %v", body["stream"])
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"text": "hi there",
			"generation_id": "gen-1",
			"finish_reason": "COMPLETE",
			"meta": {
				"tokens": {"input_tokens": 7, "output_tokens": 3}
			}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "secret", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:    "command-r",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "hi there" {
		t.Errorf("Content = %q, want %q", resp.Content, "hi there")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "stop")
	}
	if resp.ID != "gen-1" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Role != chat.RoleAssistant {
		t.Errorf("Role = %q", resp.Role)
	}
	if resp.Usage.TotalTokens != 10 {
		t.Errorf("TotalTokens = %d, want 10", resp.Usage.TotalTokens)
	}
	if resp.Usage.PromptTokens != 7 || resp.Usage.CompletionTokens != 3 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
}

// TestChat_History tests that messages are split correctly between
// the "message" field (last user message) and "chat_history".
func TestChat_History(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		// The last user message "latest question" should be in "message".
		if body["message"] != "latest question" {
			t.Errorf("message = %v", body["message"])
		}
		// The earlier messages should be in chat_history.
		history, ok := body["chat_history"].([]any)
		if !ok {
			t.Fatal("chat_history missing or wrong type")
		}
		if len(history) != 2 {
			t.Fatalf("chat_history length = %d, want 2", len(history))
		}
		h0 := history[0].(map[string]any)
		if h0["role"] != "SYSTEM" || h0["message"] != "You are helpful." {
			t.Errorf("history[0] = %v", h0)
		}
		h1 := history[1].(map[string]any)
		if h1["role"] != "USER" || h1["message"] != "earlier question" {
			t.Errorf("history[1] = %v", h1)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"text": "answer", "finish_reason": "COMPLETE"}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model: "command-r",
		Messages: []chat.Message{
			{Role: chat.RoleSystem, Content: "You are helpful."},
			{Role: chat.RoleUser, Content: "earlier question"},
			{Role: chat.RoleUser, Content: "latest question"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// TestChat_ToolCalls tests chat with tool calling.
func TestChat_ToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		// Verify tools are sent correctly.
		tools, ok := body["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("tools = %v", body["tools"])
		}
		tool := tools[0].(map[string]any)
		if tool["name"] != "get_weather" {
			t.Errorf("tool name = %v", tool["name"])
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"text": "",
			"generation_id": "gen-tc",
			"finish_reason": "COMPLETE",
			"tool_calls": [
				{"name": "get_weather", "parameters": {"city": "Paris"}}
			],
			"meta": {
				"tokens": {"input_tokens": 50, "output_tokens": 15}
			}
		}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:    "command-r",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "What's the weather in Paris?"}},
		Tools: []chat.Tool{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.Name != "get_weather" {
		t.Errorf("tool call name = %q", tc.Name)
	}
	// Parameters should be a JSON string (the marshalled object).
	if !strings.Contains(tc.Arguments, "Paris") {
		t.Errorf("tool call arguments = %q, want 'Paris' inside", tc.Arguments)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want %q", resp.FinishReason, "tool_calls")
	}
	if resp.Usage.TotalTokens != 65 {
		t.Errorf("usage total = %d, want 65", resp.Usage.TotalTokens)
	}
}

// TestChat_FinishReasonMappings tests the finish_reason conversion.
func TestChat_FinishReasonMappings(t *testing.T) {
	tests := []struct {
		name     string
		cohereFR string
		want     string
	}{
		{"complete", "COMPLETE", "stop"},
		{"max_tokens", "MAX_TOKENS", "length"},
		{"error", "ERROR", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"text":"x","finish_reason":"`+tt.cohereFR+`"}`)
			}))
			defer srv.Close()

			p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
			resp, err := p.Chat(context.Background(), chat.Request{
				Model:    "command-r",
				Messages: []chat.Message{{Role: chat.RoleUser, Content: "x"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if resp.FinishReason != tt.want {
				t.Errorf("finish_reason = %q, want %q", resp.FinishReason, tt.want)
			}
		})
	}
}

// TestChatStream_Basic tests the streaming chat path.
func TestChatStream_Basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			t.Errorf("stream flag missing: %v", body["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		events := []string{
			`event: text-generation` + "\n" + `data: {"is_finished":false,"event_type":"text-generation","text":"He"}` + "\n\n",
			`event: text-generation` + "\n" + `data: {"is_finished":false,"event_type":"text-generation","text":"llo"}` + "\n\n",
			`event: stream-end` + "\n" + `data: {"is_finished":true,"event_type":"stream-end","finish_reason":"COMPLETE","response":{"meta":{"tokens":{"input_tokens":4,"output_tokens":2}}}}` + "\n\n",
		}
		for _, ev := range events {
			io.WriteString(w, ev)
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	st, err := p.ChatStream(context.Background(), chat.Request{
		Model:    "command-r",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer st.Close()

	var deltas []string
	var done *chat.Chunk
	ctx := context.Background()
	for {
		c, err := st.Next(ctx)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			// Check for io.EOF using string comparison since we're in a test.
			if strings.Contains(err.Error(), "EOF") {
				break
			}
			t.Fatalf("Next: %v", err)
		}
		if c.Done {
			cc := c
			done = &cc
			break
		}
		deltas = append(deltas, c.Delta)
	}
	got := strings.Join(deltas, "")
	if got != "Hello" {
		t.Errorf("deltas = %q, want %q", got, "Hello")
	}
	if done == nil {
		t.Fatal("never saw Done chunk")
	}
	if done.FinishReason != "stop" {
		t.Errorf("finish_reason = %q", done.FinishReason)
	}
	if done.Usage == nil || done.Usage.TotalTokens != 6 {
		t.Errorf("usage = %+v", done.Usage)
	}
}

// TestEmbed tests the embed endpoint.
func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embed" {
			t.Errorf("path = %s, want /embed", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "embed-english-v3" {
			t.Errorf("model = %v", body["model"])
		}
		if body["input_type"] != "search_document" {
			t.Errorf("input_type = %v", body["input_type"])
		}
		texts, ok := body["texts"].([]any)
		if !ok || len(texts) != 2 {
			t.Fatalf("texts = %v", body["texts"])
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id": "emb-1",
			"texts": ["hello", "world"],
			"embeddings": [[0.1, 0.2, 0.3], [0.4, 0.5, 0.6]],
			"meta": {"billed_units": {"input_tokens": 2}}
		}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	resp, err := p.Embed(context.Background(), embed.Request{
		Model:  "embed-english-v3",
		Inputs: []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if resp.Model != "embed-english-v3" {
		t.Errorf("model = %q", resp.Model)
	}
	if len(resp.Embeddings) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(resp.Embeddings))
	}
	if resp.Embeddings[0].Index != 0 {
		t.Errorf("emb[0].Index = %d", resp.Embeddings[0].Index)
	}
	if len(resp.Embeddings[0].Vector) != 3 {
		t.Errorf("emb[0].Vector length = %d", len(resp.Embeddings[0].Vector))
	}
	if resp.Embeddings[0].Vector[0] != 0.1 {
		t.Errorf("emb[0].Vector[0] = %f", resp.Embeddings[0].Vector[0])
	}
	if resp.Embeddings[1].Vector[0] != 0.4 {
		t.Errorf("emb[1].Vector[0] = %f", resp.Embeddings[1].Vector[0])
	}
	if resp.Usage.TotalTokens != 2 {
		t.Errorf("TotalTokens = %d", resp.Usage.TotalTokens)
	}
}

// TestRerank tests the rerank endpoint.
func TestRerank(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			t.Errorf("path = %s, want /rerank", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "rerank-english-v3" {
			t.Errorf("model = %v", body["model"])
		}
		if body["query"] != "What is the capital of France?" {
			t.Errorf("query = %v", body["query"])
		}
		docs, ok := body["documents"].([]any)
		if !ok || len(docs) != 3 {
			t.Fatalf("documents = %v", body["documents"])
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"results": [
				{"index": 2, "relevance_score": 0.95},
				{"index": 0, "relevance_score": 0.50},
				{"index": 1, "relevance_score": 0.20}
			]
		}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	resp, err := p.Rerank(context.Background(), rerank.Request{
		Model: "rerank-english-v3",
		Query: "What is the capital of France?",
		Documents: []string{
			"France is a country in Europe.",
			"Berlin is the capital of Germany.",
			"Paris is the capital of France.",
		},
	})
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if resp.Model != "rerank-english-v3" {
		t.Errorf("model = %q", resp.Model)
	}
	if len(resp.Ranking) != 3 {
		t.Fatalf("expected 3 results, got %d", len(resp.Ranking))
	}
	if resp.Ranking[0].OriginalIndex != 2 {
		t.Errorf("top result index = %d, want 2", resp.Ranking[0].OriginalIndex)
	}
	if resp.Ranking[0].Score != 0.95 {
		t.Errorf("top result score = %f", resp.Ranking[0].Score)
	}
	if resp.Ranking[0].Document != "Paris is the capital of France." {
		t.Errorf("top result document = %q", resp.Ranking[0].Document)
	}
}
