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

func TestNormaliseBaseURL(t *testing.T) {
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
		if got := normaliseBaseURL(in); got != want {
			t.Errorf("normaliseBaseURL(%q) = %q, want %q", in, got, want)
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
			"model":"gpt-5.4",
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
		Model:       "gpt-5.4",
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
	if gotBody["model"] != "gpt-5.4" {
		t.Errorf("model: %v", gotBody["model"])
	}
	if _, ok := gotBody["temperature"]; !ok {
		t.Errorf("expected temperature in body")
	}
	if resp.ID != "resp-1" || resp.Model != "gpt-5.4" {
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
			"model":"gpt-5.4",
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
		Model:    "gpt-5.4",
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
			`data: {"id":"s1","model":"gpt-5.4","choices":[{"index":0,"delta":{"role":"assistant","content":"He"}}]}` + "\n\n",
			`data: {"id":"s1","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"llo"}}]}` + "\n\n",
			`: keepalive` + "\n\n",
			`data: {"id":"s1","model":"gpt-5.4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n",
			`data: {"id":"s1","model":"gpt-5.4","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}` + "\n\n",
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
		Model:    "gpt-5.4",
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
			`data: {"id":"s2","model":"gpt-5.4","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_xyz","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}` + "\n\n",
			`data: {"id":"s2","model":"gpt-5.4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"Tokyo\"}"}}]}}]}` + "\n\n",
			`data: {"id":"s2","model":"gpt-5.4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			`data: {"id":"s2","model":"gpt-5.4","choices":[],"usage":{"prompt_tokens":50,"completion_tokens":20,"total_tokens":70}}` + "\n\n",
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
		Model:    "gpt-5.4",
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
		Model:    "gpt-5.4",
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
		Model:    "gpt-5.4",
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
		Model: "gpt-5.4",
	})
	if !errors.Is(err, chat.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

// TestChat_ReasoningEffortWithTools_UsesResponsesReasoningShape covers a
// regression: tools + a non-"none" reasoning_effort routes the request to
// the /responses endpoint, which rejects the flat Chat Completions
// "reasoning_effort" field and requires it nested as reasoning.effort.
func TestChat_ReasoningEffortWithTools_UsesResponsesReasoningShape(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp-1",
			"model":"gpt-5.4",
			"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],
			"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Chat(context.Background(), chat.Request{
		Model:    "gpt-5.4",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
		Tools: []chat.Tool{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		}},
		ToolChoice: &chat.ToolChoice{Type: chat.ToolChoiceTool, Name: "get_weather"},
		ProviderOptions: map[string]any{
			"openai": openaiProviderOptions{ReasoningEffort: "medium"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/responses") {
		t.Fatalf("path = %q, want suffix /responses", gotPath)
	}
	if _, ok := gotBody["reasoning_effort"]; ok {
		t.Errorf("body still has flat reasoning_effort: %v", gotBody["reasoning_effort"])
	}
	reasoning, ok := gotBody["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("body missing nested reasoning object: %v", gotBody)
	}
	if reasoning["effort"] != "medium" {
		t.Errorf("reasoning.effort = %v, want %q", reasoning["effort"], "medium")
	}

	// The Responses API flattens tool/tool_choice: no nested "function" key.
	tools, ok := gotBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v", gotBody["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	if _, hasNested := tool["function"]; hasNested {
		t.Errorf("tools[0] still has nested function key: %v", tool)
	}
	if tool["name"] != "get_weather" {
		t.Errorf("tools[0].name = %v, want get_weather", tool["name"])
	}
	toolChoice, _ := gotBody["tool_choice"].(map[string]any)
	if _, hasNested := toolChoice["function"]; hasNested {
		t.Errorf("tool_choice still has nested function key: %v", toolChoice)
	}
	if toolChoice["name"] != "get_weather" {
		t.Errorf("tool_choice.name = %v, want get_weather", toolChoice["name"])
	}
}

// TestChat_ResponsesAPI_TopLevelFunctionCall covers a regression: the
// Responses API returns a tool call as its own top-level output item
// (type: "function_call") with call_id/name/arguments set directly on the
// item, not nested inside a "message" item's content blocks.
func TestChat_ResponsesAPI_TopLevelFunctionCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp-2",
			"model":"gpt-5.4",
			"output":[
				{"type":"reasoning","summary":"thinking"},
				{"type":"function_call","call_id":"call_xyz","name":"get_weather","arguments":"{\"city\":\"Paris\"}"}
			],
			"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:    "gpt-5.4",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
		Tools: []chat.Tool{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		}},
		ProviderOptions: map[string]any{
			"openai": openaiProviderOptions{ReasoningEffort: "low"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %+v", len(resp.ToolCalls), resp.ToolCalls)
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_xyz" || tc.Name != "get_weather" || tc.Arguments != `{"city":"Paris"}` {
		t.Errorf("tool call = %+v", tc)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", resp.FinishReason)
	}
}

// TestChat_ResponsesAPI_RenamesAndDropsUnsupportedFields covers a
// regression: the Responses API renames max_tokens to max_output_tokens,
// and rejects stop/temperature/top_p outright on reasoning-effort requests
// (reasoning models use fixed sampling), so all three must be dropped
// rather than passed through unchanged from Chat Completions.
func TestChat_ResponsesAPI_RenamesAndDropsUnsupportedFields(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp-3",
			"model":"gpt-5.4",
			"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],
			"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Chat(context.Background(), chat.Request{
		Model:       "gpt-5.4",
		Messages:    []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   64,
		Stop:        []string{"###"},
		Tools: []chat.Tool{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		}},
		ProviderOptions: map[string]any{
			"openai": openaiProviderOptions{ReasoningEffort: "low"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	for _, unsupported := range []string{"stop", "temperature", "top_p", "max_tokens"} {
		if v, ok := gotBody[unsupported]; ok {
			t.Errorf("body still has %s: %v", unsupported, v)
		}
	}
	if gotBody["max_output_tokens"] != float64(64) {
		t.Errorf("max_output_tokens = %v, want 64", gotBody["max_output_tokens"])
	}
}
