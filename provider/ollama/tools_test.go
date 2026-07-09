package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

func TestChat_ToolCallNonStream(t *testing.T) {
	var rawBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":      "llama3",
			"created_at": "now",
			"message": map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"function": map[string]any{
							"name":      "get_time",
							"arguments": map[string]any{"tz": "UTC"},
						},
					},
				},
			},
			"done":        true,
			"done_reason": "stop",
		})
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:    "llama3",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "what time is it?"}},
		Tools: []chat.Tool{{
			Name:        "get_time",
			Description: "current time",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"tz":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if _, ok := rawBody["tools"]; !ok {
		t.Errorf("tools missing from request body: %v", rawBody)
	}
	if _, ok := rawBody["tool_choice"]; ok {
		t.Errorf("tool_choice unexpectedly present in request body")
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_0" {
		t.Errorf("ID = %q want call_0", tc.ID)
	}
	if tc.Name != "get_time" {
		t.Errorf("Name = %q", tc.Name)
	}
	var gotArgs, wantArgs map[string]any
	if err := json.Unmarshal([]byte(tc.Arguments), &gotArgs); err != nil {
		t.Fatalf("arguments not valid JSON: %v (%q)", err, tc.Arguments)
	}
	_ = json.Unmarshal([]byte(`{"tz":"UTC"}`), &wantArgs)
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Errorf("arguments = %v want %v", gotArgs, wantArgs)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q want tool_calls", resp.FinishReason)
	}
}

func TestChat_ToolMessagesRoundTrip(t *testing.T) {
	var rawBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":       "llama3",
			"message":     map[string]any{"role": "assistant", "content": "ok"},
			"done":        true,
			"done_reason": "stop",
		})
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model: "llama3",
		Messages: []chat.Message{
			{Role: chat.RoleUser, Content: "what time is it?"},
			{
				Role: chat.RoleAssistant,
				ToolCalls: []chat.ToolCall{
					{ID: "call_0", Name: "get_time", Arguments: `{"tz":"UTC"}`},
				},
			},
			{Role: chat.RoleTool, ToolCallID: "call_0", Content: "12:00:00Z"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	msgs, ok := rawBody["messages"].([]any)
	if !ok || len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %v", rawBody["messages"])
	}

	asst, _ := msgs[1].(map[string]any)
	if asst["role"] != "assistant" {
		t.Errorf("msg[1].role = %v", asst["role"])
	}
	tcs, ok := asst["tool_calls"].([]any)
	if !ok || len(tcs) != 1 {
		t.Fatalf("assistant tool_calls = %v", asst["tool_calls"])
	}
	tc0, _ := tcs[0].(map[string]any)
	fn, _ := tc0["function"].(map[string]any)
	if fn["name"] != "get_time" {
		t.Errorf("function.name = %v", fn["name"])
	}
	// arguments must be an OBJECT, not a string.
	argsObj, ok := fn["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("arguments is not an object: %T %v", fn["arguments"], fn["arguments"])
	}
	if argsObj["tz"] != "UTC" {
		t.Errorf("arguments.tz = %v", argsObj["tz"])
	}

	tool, _ := msgs[2].(map[string]any)
	if tool["role"] != "tool" {
		t.Errorf("msg[2].role = %v", tool["role"])
	}
	if tool["content"] != "12:00:00Z" {
		t.Errorf("msg[2].content = %v", tool["content"])
	}
	if _, has := tool["tool_call_id"]; has {
		t.Errorf("tool_call_id should not be sent on the wire, got %v", tool["tool_call_id"])
	}
}

func TestChatStream_ToolCallChunk(t *testing.T) {
	body := strings.Join([]string{
		`{"model":"llama3","message":{"role":"assistant","content":"thinking..."},"done":false}`,
		`{"model":"llama3","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"get_time","arguments":{"tz":"UTC"}}}]},"done":false}`,
		`{"model":"llama3","done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":2}`,
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

	var sawToolDelta bool
	var finalChunk chat.Chunk
	for {
		c, err := st.Next(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if len(c.ToolCallDeltas) > 0 {
			sawToolDelta = true
			d := c.ToolCallDeltas[0]
			if d.Index != 0 || d.ID != "call_0" || d.Name != "get_time" {
				t.Errorf("delta = %+v", d)
			}
			var probe map[string]any
			if err := json.Unmarshal([]byte(d.ArgsDelta), &probe); err != nil {
				t.Errorf("ArgsDelta not valid JSON: %v (%q)", err, d.ArgsDelta)
			}
			if probe["tz"] != "UTC" {
				t.Errorf("ArgsDelta.tz = %v", probe["tz"])
			}
		}
		if c.Done {
			finalChunk = c
			break
		}
	}
	if !sawToolDelta {
		t.Errorf("expected a chunk with ToolCallDeltas")
	}
	if !finalChunk.Done {
		t.Fatalf("expected final done chunk")
	}
	if finalChunk.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q want tool_calls", finalChunk.FinishReason)
	}
}

func TestChat_ToolsRequestBody(t *testing.T) {
	var rawBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":       "llama3",
			"message":     map[string]any{"role": "assistant", "content": "ok"},
			"done":        true,
			"done_reason": "stop",
		})
	}))
	defer srv.Close()

	params := json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}},"required":["x"]}`)
	p := New(Config{BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model:    "llama3",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
		Tools: []chat.Tool{{
			Name:        "do_thing",
			Description: "does a thing",
			Parameters:  params,
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	tools, ok := rawBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v", rawBody["tools"])
	}
	t0, _ := tools[0].(map[string]any)
	if t0["type"] != "function" {
		t.Errorf("type = %v", t0["type"])
	}
	fn, _ := t0["function"].(map[string]any)
	if fn["name"] != "do_thing" {
		t.Errorf("function.name = %v", fn["name"])
	}
	gotParams, _ := json.Marshal(fn["parameters"])
	var wantNorm, gotNorm any
	if err := json.Unmarshal(params, &wantNorm); err != nil {
		t.Fatalf("normalise want: %v", err)
	}
	if err := json.Unmarshal(gotParams, &gotNorm); err != nil {
		t.Fatalf("normalise got: %v", err)
	}
	if !reflect.DeepEqual(gotNorm, wantNorm) {
		t.Errorf("parameters = %s want %s", gotParams, params)
	}
}

func TestChat_ToolChoiceIgnoredCleanly(t *testing.T) {
	var rawBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&rawBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":       "llama3",
			"message":     map[string]any{"role": "assistant", "content": "ok"},
			"done":        true,
			"done_reason": "stop",
		})
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:      "llama3",
		Messages:   []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
		ToolChoice: &chat.ToolChoice{Type: chat.ToolChoiceRequired},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q", resp.Content)
	}
	if _, has := rawBody["tool_choice"]; has {
		t.Errorf("tool_choice should be silently dropped from wire, got %v", rawBody["tool_choice"])
	}
}
