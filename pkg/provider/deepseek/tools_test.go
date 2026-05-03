package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

// TestChat_ToolCallNonStream verifies that tool_calls in a non-streaming
// response are decoded into Response.ToolCalls and that an absent
// finish_reason is upgraded to "tool_calls".
func TestChat_ToolCallNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp-tc",
			"model":"deepseek-chat",
			"choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[
				{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}
			]},"finish_reason":"tool_calls"}],
			"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}
		}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:    "deepseek-chat",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "weather?"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1: %+v", len(resp.ToolCalls), resp.ToolCalls)
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc" || tc.Name != "get_weather" || tc.Arguments != `{"city":"Paris"}` {
		t.Errorf("ToolCall = %+v", tc)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", resp.FinishReason)
	}
}

// TestChat_ToolMessagesOnWire asserts the assistant tool_calls and tool
// result message are serialised in the OpenAI-compatible shape.
func TestChat_ToolMessagesOnWire(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model: "m",
		Messages: []chat.Message{
			{Role: chat.RoleUser, Content: "weather?"},
			{Role: chat.RoleAssistant, Content: "", ToolCalls: []chat.ToolCall{
				{ID: "call_abc", Name: "get_weather", Arguments: `{"city":"Paris"}`},
			}},
			{Role: chat.RoleTool, Content: "sunny", ToolCallID: "call_abc"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	var body map[string]any
	if jerr := json.Unmarshal(captured, &body); jerr != nil {
		t.Fatalf("decode body: %v", jerr)
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages len = %d", len(msgs))
	}
	asst, _ := msgs[1].(map[string]any)
	if asst["role"] != "assistant" {
		t.Errorf("assistant role = %v", asst["role"])
	}
	tcs, _ := asst["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("tool_calls len = %d", len(tcs))
	}
	tc0, _ := tcs[0].(map[string]any)
	if tc0["id"] != "call_abc" || tc0["type"] != "function" {
		t.Errorf("tc0 id/type = %+v", tc0)
	}
	fn, _ := tc0["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("function.name = %v", fn["name"])
	}
	args, ok := fn["arguments"].(string)
	if !ok {
		t.Fatalf("function.arguments must be a STRING on wire, got %T (%v)", fn["arguments"], fn["arguments"])
	}
	if args != `{"city":"Paris"}` {
		t.Errorf("function.arguments = %q", args)
	}

	tool, _ := msgs[2].(map[string]any)
	if tool["role"] != "tool" {
		t.Errorf("tool role = %v", tool["role"])
	}
	if tool["content"] != "sunny" {
		t.Errorf("tool content = %v", tool["content"])
	}
	if tool["tool_call_id"] != "call_abc" {
		t.Errorf("tool_call_id = %v", tool["tool_call_id"])
	}
}

// TestChat_ToolChoice covers all valid mappings plus the validation error.
func TestChat_ToolChoice(t *testing.T) {
	type tcase struct {
		name    string
		choice  *chat.ToolChoice
		wantKey bool
		check   func(t *testing.T, v any)
	}
	cases := []tcase{
		{
			name:    "default-nil-omits",
			choice:  nil,
			wantKey: false,
		},
		{
			name:    "auto",
			choice:  &chat.ToolChoice{Type: chat.ToolChoiceAuto},
			wantKey: true,
			check: func(t *testing.T, v any) {
				if v != "auto" {
					t.Errorf("tool_choice = %v", v)
				}
			},
		},
		{
			name:    "none",
			choice:  &chat.ToolChoice{Type: chat.ToolChoiceNone},
			wantKey: true,
			check: func(t *testing.T, v any) {
				if v != "none" {
					t.Errorf("tool_choice = %v", v)
				}
			},
		},
		{
			name:    "required",
			choice:  &chat.ToolChoice{Type: chat.ToolChoiceRequired},
			wantKey: true,
			check: func(t *testing.T, v any) {
				if v != "required" {
					t.Errorf("tool_choice = %v", v)
				}
			},
		},
		{
			name:    "specific-tool",
			choice:  &chat.ToolChoice{Type: chat.ToolChoiceTool, Name: "get_weather"},
			wantKey: true,
			check: func(t *testing.T, v any) {
				m, ok := v.(map[string]any)
				if !ok {
					t.Fatalf("tool_choice not object: %T", v)
				}
				if m["type"] != "function" {
					t.Errorf("type = %v", m["type"])
				}
				fn, _ := m["function"].(map[string]any)
				if fn["name"] != "get_weather" {
					t.Errorf("function.name = %v", fn["name"])
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
			}))
			defer srv.Close()

			p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
			_, err := p.Chat(context.Background(), chat.Request{
				Model:      "m",
				Messages:   []chat.Message{{Role: chat.RoleUser, Content: "x"}},
				ToolChoice: c.choice,
			})
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}
			v, present := body["tool_choice"]
			if c.wantKey != present {
				t.Fatalf("tool_choice present = %v, want %v (value=%v)", present, c.wantKey, v)
			}
			if c.check != nil {
				c.check(t, v)
			}
		})
	}

	t.Run("specific-tool-empty-name", func(t *testing.T) {
		p, _ := New(Config{APIKey: "k", BaseURL: "http://example.invalid"})
		_, err := p.Chat(context.Background(), chat.Request{
			Model:      "m",
			Messages:   []chat.Message{{Role: chat.RoleUser, Content: "x"}},
			ToolChoice: &chat.ToolChoice{Type: chat.ToolChoiceTool, Name: ""},
		})
		if !errors.Is(err, chat.ErrInvalidRequest) {
			t.Fatalf("expected ErrInvalidRequest, got %v", err)
		}
	})
}

// TestChat_ToolMessageMissingID — a RoleTool message without ToolCallID
// must produce ErrInvalidRequest before the request is dispatched.
func TestChat_ToolMessageMissingID(t *testing.T) {
	p, _ := New(Config{APIKey: "k", BaseURL: "http://example.invalid"})
	_, err := p.Chat(context.Background(), chat.Request{
		Model: "m",
		Messages: []chat.Message{
			{Role: chat.RoleUser, Content: "x"},
			{Role: chat.RoleTool, Content: "result"},
		},
	})
	if !errors.Is(err, chat.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

// TestChatStream_ToolCallDeltas verifies SSE tool-call deltas are emitted
// to the consumer and that the final Done chunk carries finish_reason
// "tool_calls" plus the usage payload from the trailing usage-only chunk.
func TestChatStream_ToolCallDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		writes := []string{
			// First delta introduces index 0 with id+name.
			`data: {"id":"s1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}` + "\n\n",
			// Argument fragments.
			`data: {"id":"s1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"ci"}}]}}]}` + "\n\n",
			`data: {"id":"s1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ty\":\"Paris\"}"}}]}}]}` + "\n\n",
			// finish_reason chunk.
			`data: {"id":"s1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			// usage-only trailing chunk.
			`data: {"id":"s1","model":"m","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n",
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

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	st, err := p.ChatStream(context.Background(), chat.Request{
		Model:    "m",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "weather?"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer st.Close()

	var allDeltas []chat.ToolCallDelta
	var done *chat.Chunk
	ctx := context.Background()
	for {
		c, err := st.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		allDeltas = append(allDeltas, c.ToolCallDeltas...)
		if c.Done {
			cc := c
			done = &cc
			break
		}
	}
	if len(allDeltas) != 3 {
		t.Fatalf("ToolCallDeltas count = %d, want 3: %+v", len(allDeltas), allDeltas)
	}
	if allDeltas[0].ID != "call_abc" || allDeltas[0].Name != "get_weather" || allDeltas[0].Index != 0 {
		t.Errorf("delta[0] = %+v", allDeltas[0])
	}
	if allDeltas[1].ArgsDelta != `{"ci` || allDeltas[2].ArgsDelta != `ty":"Paris"}` {
		t.Errorf("arg fragments: %q / %q", allDeltas[1].ArgsDelta, allDeltas[2].ArgsDelta)
	}
	// Reconstruct via the consumer helper.
	tcs := chat.AssembleToolCalls(allDeltas)
	if len(tcs) != 1 || tcs[0].ID != "call_abc" || tcs[0].Name != "get_weather" || tcs[0].Arguments != `{"city":"Paris"}` {
		t.Errorf("AssembleToolCalls = %+v", tcs)
	}
	if done == nil {
		t.Fatal("never saw Done chunk")
	}
	if done.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q", done.FinishReason)
	}
	if done.Usage == nil || done.Usage.TotalTokens != 15 {
		t.Errorf("Usage on done = %+v", done.Usage)
	}
}

// TestChatStream_ParallelToolCalls interleaves two tool calls (indices 0
// and 1). AssembleToolCalls is used to verify reconstruction in index
// order, regardless of the order deltas arrived in.
func TestChatStream_ParallelToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		writes := []string{
			// Introduce index 0.
			`data: {"id":"s","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"tool_a","arguments":""}}]}}]}` + "\n\n",
			// Introduce index 1.
			`data: {"id":"s","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"tool_b","arguments":""}}]}}]}` + "\n\n",
			// Interleaved arg fragments — note index 1 arrives first.
			`data: {"id":"s","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"y\":2}"}}]}}]}` + "\n\n",
			`data: {"id":"s","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"x\":"}}]}}]}` + "\n\n",
			`data: {"id":"s","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}` + "\n\n",
			`data: {"id":"s","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
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

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	st, err := p.ChatStream(context.Background(), chat.Request{
		Model:    "m",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "go"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer st.Close()

	var deltas []chat.ToolCallDelta
	ctx := context.Background()
	for {
		c, err := st.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		deltas = append(deltas, c.ToolCallDeltas...)
		if c.Done {
			break
		}
	}
	tcs := chat.AssembleToolCalls(deltas)
	if len(tcs) != 2 {
		t.Fatalf("AssembleToolCalls len = %d, want 2: %+v", len(tcs), tcs)
	}
	// Sort defensively, although AssembleToolCalls is documented to
	// return index-ascending order.
	sort.Slice(tcs, func(i, j int) bool { return tcs[i].ID < tcs[j].ID })
	if tcs[0].ID != "call_a" || tcs[0].Name != "tool_a" || tcs[0].Arguments != `{"x":1}` {
		t.Errorf("call_a = %+v", tcs[0])
	}
	if tcs[1].ID != "call_b" || tcs[1].Name != "tool_b" || tcs[1].Arguments != `{"y":2}` {
		t.Errorf("call_b = %+v", tcs[1])
	}
}
