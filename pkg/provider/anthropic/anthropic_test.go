package anthropic

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

func TestChat_NonStreaming(t *testing.T) {
	var gotPath, gotAPIKey, gotVersion string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_01",
			"model":"claude-sonnet-4-20250514",
			"role":"assistant",
			"content":[{"type":"text","text":"hi there"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":7,"output_tokens":3}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "secret", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotPath != "/v1/messages" {
		t.Errorf("path: %s", gotPath)
	}
	if gotAPIKey != "secret" {
		t.Errorf("x-api-key: %s", gotAPIKey)
	}
	if gotVersion != "2023-06-01" {
		t.Errorf("anthropic-version: %s", gotVersion)
	}
	if gotBody["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model: %v", gotBody["model"])
	}
	if gotBody["stream"] != nil {
		t.Errorf("stream should not be set for non-streaming: %v", gotBody["stream"])
	}
	maxToks, _ := gotBody["max_tokens"].(float64)
	if maxToks != float64(defaultMaxTokens) {
		t.Errorf("max_tokens: got %v want %d", maxToks, defaultMaxTokens)
	}
	if resp.ID != "msg_01" || resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("resp meta: %+v", resp)
	}
	if resp.Content != "hi there" || resp.FinishReason != "stop" || resp.Role != chat.RoleAssistant {
		t.Errorf("resp body: content=%q finish=%q role=%q", resp.Content, resp.FinishReason, resp.Role)
	}
	if resp.Usage.TotalTokens != 10 {
		t.Errorf("usage total: got %d want 10", resp.Usage.TotalTokens)
	}
}

func TestChat_ToolUse(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_02",
			"model":"claude-sonnet-4-20250514",
			"role":"assistant",
			"content":[
				{"type":"text","text":"Let me check."},
				{"type":"tool_use","id":"toolu_01","name":"get_weather","input":{"location":"SF"}}
			],
			"stop_reason":"tool_use",
			"usage":{"input_tokens":15,"output_tokens":8}
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Chat(context.Background(), chat.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "weather?"}},
		Tools: []chat.Tool{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
		}},
		ToolChoice: &chat.ToolChoice{Type: chat.ToolChoiceAuto},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	// Verify tools were sent correctly.
	tools, _ := gotBody["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools len: got %d want 1", len(tools))
	}
	tool0 := tools[0].(map[string]any)
	if tool0["name"] != "get_weather" {
		t.Errorf("tool name: %v", tool0["name"])
	}
	tc, _ := gotBody["tool_choice"].(map[string]any)
	if tc["type"] != "auto" {
		t.Errorf("tool_choice type: %v", tc["type"])
	}
	// Verify response.
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len: got %d want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "toolu_01" {
		t.Errorf("tool call ID: %s", resp.ToolCalls[0].ID)
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("tool call name: %s", resp.ToolCalls[0].Name)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("finish_reason: %q", resp.FinishReason)
	}
	if resp.Content != "Let me check." {
		t.Errorf("content: %q", resp.Content)
	}
}

func TestChat_SystemMessage(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_03","model":"claude-sonnet-4-20250514","role":"assistant",
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":5,"output_tokens":1}
		}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []chat.Message{
			{Role: chat.RoleSystem, Content: "be helpful"},
			{Role: chat.RoleUser, Content: "hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sys, ok := gotBody["system"].(string)
	if !ok || sys != "be helpful" {
		t.Errorf("system: %v", gotBody["system"])
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages len: got %d want 1", len(msgs))
	}
}

func TestChat_ToolResult(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_04","model":"claude-sonnet-4-20250514","role":"assistant",
			"content":[{"type":"text","text":"It is 72F."}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":10,"output_tokens":3}
		}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []chat.Message{
			{Role: chat.RoleUser, Content: "weather?"},
			{Role: chat.RoleAssistant, ToolCalls: []chat.ToolCall{
				{ID: "toolu_01", Name: "get_weather", Arguments: `{"location":"SF"}`},
			}},
			{Role: chat.RoleTool, ToolCallID: "toolu_01", Content: "Sunny, 72F"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages: got %d want 3", len(msgs))
	}
	// Check tool_result message.
	msg2 := msgs[2].(map[string]any)
	role, _ := msg2["role"].(string)
	if role != "user" {
		t.Errorf("tool result role: %q", role)
	}
	content, _ := msg2["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("tool result content len: %d", len(content))
	}
	cb := content[0].(map[string]any)
	if cb["type"] != "tool_result" || cb["tool_use_id"] != "toolu_01" {
		t.Errorf("tool result block: %+v", cb)
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
			`event: message_start` + "\n" +
				`data: {"type":"message_start","message":{"id":"msg_s1","model":"claude-sonnet-4-20250514","role":"assistant","usage":{"input_tokens":4}}}` + "\n\n",
			`event: content_block_start` + "\n" +
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n",
			`event: content_block_delta` + "\n" +
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"He"}}` + "\n\n",
			`: keepalive` + "\n\n",
			`event: content_block_delta` + "\n" +
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"llo"}}` + "\n\n",
			`event: content_block_stop` + "\n" +
				`data: {"type":"content_block_stop","index":0}` + "\n\n",
			`event: message_delta` + "\n" +
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}` + "\n\n",
			`event: message_stop` + "\n" +
				`data: {"type":"message_stop"}` + "\n\n",
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
		Model:    "claude-sonnet-4-20250514",
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

func TestChatStream_ToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		writes := []string{
			`event: message_start` + "\n" +
				`data: {"type":"message_start","message":{"id":"msg_st","model":"claude-sonnet-4-20250514","role":"assistant","usage":{"input_tokens":10}}}` + "\n\n",
			`event: content_block_start` + "\n" +
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n",
			`event: content_block_delta` + "\n" +
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me check."}}` + "\n\n",
			`event: content_block_stop` + "\n" +
				`data: {"type":"content_block_stop","index":0}` + "\n\n",
			`event: content_block_start` + "\n" +
				`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_01","name":"get_weather","input":{}}}` + "\n\n",
			`event: content_block_delta` + "\n" +
				`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"loc"}}` + "\n\n",
			`event: content_block_delta` + "\n" +
				`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"ation\":\"SF\"}"}}` + "\n\n",
			`event: content_block_stop` + "\n" +
				`data: {"type":"content_block_stop","index":1}` + "\n\n",
			`event: message_delta` + "\n" +
				`data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":12}}` + "\n\n",
			`event: message_stop` + "\n" +
				`data: {"type":"message_stop"}` + "\n\n",
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
		Model:    "claude-sonnet-4-20250514",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "weather?"}},
		Tools: []chat.Tool{{
			Name:       "get_weather",
			Parameters: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer st.Close()

	var deltas []string
	var toolDeltas []chat.ToolCallDelta
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
			if c.Delta != "" {
				deltas = append(deltas, c.Delta)
			}
			toolDeltas = append(toolDeltas, c.ToolCallDeltas...)
		}
		if c.Done {
			_, err2 := st.Next(ctx)
			if !errors.Is(err2, io.EOF) {
				t.Fatalf("expected EOF after Done, got err=%v", err2)
			}
			break
		}
	}
	if got := strings.Join(deltas, ""); got != "Let me check." {
		t.Errorf("text deltas: %q", got)
	}
	if len(toolDeltas) != 2 {
		t.Fatalf("tool deltas count: got %d want 2", len(toolDeltas))
	}
	var args string
	for _, td := range toolDeltas {
		args += td.ArgsDelta
	}
	if args != `{"location":"SF"}` {
		t.Errorf("accumulated tool args: %q", args)
	}
	if doneChunk == nil {
		t.Fatal("no done chunk")
	}
	if doneChunk.FinishReason != "tool_calls" {
		t.Errorf("finish_reason: %q", doneChunk.FinishReason)
	}
}

func TestChat_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model:    "m",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "x"}},
	})
	if !errors.Is(err, chat.ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got %v", err)
	}
}

func TestChat_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model:    "m",
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

func TestChat_Image(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_img","model":"claude-sonnet-4-20250514","role":"assistant",
			"content":[{"type":"text","text":"I see an image."}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":50,"output_tokens":5}
		}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []chat.Message{{
			Role: chat.RoleUser,
			Parts: chat.Parts{
				chat.TextPart{Text: "Describe this:"},
				chat.ImagePart{Data: []byte{0x89, 0x50, 0x4E, 0x47}, MediaType: "image/png"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages: got %d want 1", len(msgs))
	}
	msg := msgs[0].(map[string]any)
	content, _ := msg["content"].([]any)
	if len(content) < 2 {
		t.Fatalf("content blocks: got %d want >= 2", len(content))
	}
	textBlock := content[0].(map[string]any)
	if textBlock["type"] != "text" || textBlock["text"] != "Describe this:" {
		t.Errorf("text block: %+v", textBlock)
	}
	imgBlock := content[1].(map[string]any)
	if imgBlock["type"] != "image" {
		t.Errorf("image block type: %v", imgBlock["type"])
	}
}

func TestChat_ToolChoiceTool(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_tct","model":"claude-sonnet-4-20250514","role":"assistant",
			"content":[{"type":"tool_use","id":"toolu_02","name":"get_weather","input":{"location":"NYC"}}],
			"stop_reason":"tool_use",
			"usage":{"input_tokens":5,"output_tokens":8}
		}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), chat.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "weather?"}},
		Tools: []chat.Tool{{
			Name:       "get_weather",
			Parameters: json.RawMessage(`{"type":"object"}`),
		}},
		ToolChoice: &chat.ToolChoice{Type: chat.ToolChoiceTool, Name: "get_weather"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tc, _ := gotBody["tool_choice"].(map[string]any)
	if tc["type"] != "tool" || tc["name"] != "get_weather" {
		t.Errorf("tool_choice: %+v", tc)
	}
}

// guard against accidental import of fmt only
var _ = fmt.Sprintf
