package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

func newTestInteractions(t *testing.T, h http.HandlerFunc) *Interactions {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p, err := NewInteractions(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
	if err != nil {
		t.Fatal(err)
	}
	p.pollInterval = 0
	return p
}

func TestInteractionsRequestEncoding(t *testing.T) {
	thought := json.RawMessage(`{"type":"thought","signature":"sig"}`)
	tests := []struct {
		name     string
		req      chat.Request
		want     string
		wantWarn bool
	}{
		{
			name: "stateless history with replayed steps and tool result",
			req: chat.Request{
				Model: "gemini-4-argon",
				Messages: []chat.Message{
					{Role: chat.RoleSystem, Content: "be brief"},
					{Role: chat.RoleUser, Content: "weather?"},
					{
						Role:            chat.RoleAssistant,
						ToolCalls:       []chat.ToolCall{{ID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`}},
						ProviderOptions: map[string]any{"gemini": map[string]any{"interaction_steps": []any{thought}}},
					},
					{Role: chat.RoleTool, ToolCallID: "c1", Content: "sunny"},
				},
				Tools:           []chat.Tool{{Name: "get_weather", Parameters: json.RawMessage(`{"type":"object"}`)}},
				ToolChoice:      &chat.ToolChoice{Type: chat.ToolChoiceTool, Name: "get_weather"},
				ProviderOptions: map[string]any{"gemini": map[string]any{"thinking_level": "high"}},
			},
			want: `{"model":"gemini-4-argon","store":false,"system_instruction":"be brief",
				"input":[
					{"type":"user_input","content":[{"type":"text","text":"weather?"}]},
					{"type":"thought","signature":"sig"},
					{"type":"function_result","name":"get_weather","call_id":"c1","result":[{"type":"text","text":"sunny"}]}],
				"tools":[{"type":"function","name":"get_weather","parameters":{"type":"object"}}],
				"generation_config":{"thinking_level":"high",
					"tool_choice":{"allowed_tools":{"mode":"any","tools":["get_weather"]}}}}`,
		},
		{
			name: "synthesised assistant steps without replay",
			req: chat.Request{
				Model: "m",
				Messages: []chat.Message{
					{Role: chat.RoleUser, Content: "hi"},
					{Role: chat.RoleAssistant, Content: "hello", ToolCalls: []chat.ToolCall{{ID: "c1", Name: "f"}}},
				},
			},
			want: `{"model":"m","store":false,"input":[
				{"type":"user_input","content":[{"type":"text","text":"hi"}]},
				{"type":"model_output","content":[{"type":"text","text":"hello"}]},
				{"type":"function_call","id":"c1","name":"f","arguments":{}}]}`,
		},
		{
			name: "background option runs long decode with storage",
			req: chat.Request{
				Model: "gemini-4-argon", MaxTokens: 262144, Messages: []chat.Message{{Role: chat.RoleUser, Content: "x"}},
				ProviderOptions: map[string]any{"gemini": map[string]any{"background": true}},
			},
			want: `{"model":"gemini-4-argon","store":true,"background":true,
				"input":[{"type":"user_input","content":[{"type":"text","text":"x"}]}],
				"generation_config":{"max_output_tokens":262144}}`,
		},
		{
			name:     "max tokens above foreground limit clamps without background",
			req:      chat.Request{Model: "gemini-4-argon", MaxTokens: 262144, Messages: []chat.Message{{Role: chat.RoleUser, Content: "x"}}},
			want:     `{"model":"gemini-4-argon","store":false,"input":[{"type":"user_input","content":[{"type":"text","text":"x"}]}],"generation_config":{"max_output_tokens":65536}}`,
			wantWarn: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []byte
			p := newTestInteractions(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("x-goog-api-key") != "k" || r.Header.Get("Api-Revision") != interactionsAPIRevision {
					t.Errorf("headers: %v", r.Header)
				}
				if r.Method == http.MethodPost {
					got, _ = io.ReadAll(r.Body)
				}
				_, _ = io.WriteString(w, `{"id":"i1","status":"completed","steps":[]}`)
			})
			resp, err := p.Chat(context.Background(), tt.req)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(resp.Warnings) > 0; got != tt.wantWarn {
				t.Errorf("warnings = %v, want present=%v", resp.Warnings, tt.wantWarn)
			}
			var gotV, wantV any
			if err := json.Unmarshal(got, &gotV); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tt.want), &wantV); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotV, wantV) {
				t.Errorf("body\n got: %s\nwant: %s", got, tt.want)
			}
		})
	}
}

func TestInteractionsChatResponse(t *testing.T) {
	const completed = `{"id":"i1","status":"requires_action","steps":[
		{"type":"user_input","content":[{"type":"text","text":"q"}]},
		{"type":"thought","signature":"sig","summary":[{"type":"text","text":"thinking"}]},
		{"type":"model_output","content":[{"type":"text","text":"Let me check."}]},
		{"type":"function_call","id":"c1","name":"get_weather","arguments":{"city":"Paris"}}],
		"usage":{"total_input_tokens":5,"total_output_tokens":3,"total_thought_tokens":2,"total_tokens":10}}`
	tests := []struct {
		name       string
		background bool
		calls      []string
		wantErr    error
		status     string
	}{
		{name: "foreground", calls: []string{"POST /v1beta/interactions"}},
		{
			name: "background polls then deletes", background: true,
			calls: []string{"POST /v1beta/interactions", "GET /v1beta/interactions/i1", "DELETE /v1beta/interactions/i1"},
		},
		{
			name: "failed interaction", status: "failed", wantErr: chat.ErrProviderUnavailable,
			calls: []string{"POST /v1beta/interactions"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var calls []string
			p := newTestInteractions(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls = append(calls, r.Method+" "+r.URL.Path)
				mu.Unlock()
				switch {
				case tt.status != "":
					_, _ = io.WriteString(w, `{"id":"i1","status":"`+tt.status+`","errors":[{"message":"boom"}]}`)
				case r.Method == http.MethodPost && tt.background:
					_, _ = io.WriteString(w, `{"id":"i1","status":"in_progress"}`)
				case r.Method == http.MethodDelete:
				default:
					_, _ = io.WriteString(w, completed)
				}
			})
			resp, err := p.Chat(context.Background(), chat.Request{
				Model: "gemini-4-argon", Messages: []chat.Message{{Role: chat.RoleUser, Content: "q"}},
				ProviderOptions: map[string]any{"gemini": interactionsOptions{Background: tt.background}},
			})
			if !reflect.DeepEqual(calls, tt.calls) {
				t.Errorf("calls = %v, want %v", calls, tt.calls)
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := chat.Response{
				ID: "i1", Model: "gemini-4-argon", Role: chat.RoleAssistant, Content: "Let me check.",
				Parts:        chat.Parts{chat.ReasoningPart{Text: "thinking"}, chat.TextPart{Text: "Let me check."}},
				ToolCalls:    []chat.ToolCall{{ID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`}},
				FinishReason: "tool_calls",
				Usage:        chat.Usage{PromptTokens: 5, CompletionTokens: 5, TotalTokens: 10},
			}
			steps := resp.ProviderMetadata["gemini"].(interactionsMessageOptions).InteractionSteps
			resp.ProviderMetadata = nil
			if !reflect.DeepEqual(resp, want) {
				t.Errorf("response\n got: %+v\nwant: %+v", resp, want)
			}
			if len(steps) != 3 {
				t.Errorf("replay steps = %d, want 3 (user_input excluded)", len(steps))
			}
		})
	}
}

func TestInteractionsStream(t *testing.T) {
	events := []string{
		`{"event_type":"interaction.created","event_id":"e1","interaction":{"id":"i1","status":"in_progress"}}`,
		`{"event_type":"step.start","event_id":"e2","index":0,"step":{"type":"thought"}}`,
		`{"event_type":"step.delta","event_id":"e3","index":0,"delta":{"type":"thought_summary","content":{"type":"text","text":"hmm"}}}`,
		`{"event_type":"step.delta","event_id":"e4","index":0,"delta":{"type":"thought_signature","signature":"sig"}}`,
		`{"event_type":"step.start","event_id":"e5","index":1,"step":{"type":"model_output"}}`,
		`{"event_type":"step.delta","event_id":"e6","index":1,"delta":{"type":"text","text":"Hi "}}`,
		`{"event_type":"step.start","event_id":"e7","index":2,"step":{"type":"function_call","id":"c1","name":"f"}}`,
		`{"event_type":"step.delta","event_id":"e8","index":2,"delta":{"type":"arguments_delta","arguments":"{\"a\":"}}`,
		`{"event_type":"step.delta","event_id":"e9","index":2,"delta":{"type":"arguments_delta","arguments":"1}"}}`,
		`{"event_type":"interaction.completed","event_id":"e10","interaction":{"id":"i1","status":"requires_action","usage":{"total_tokens":7}}}`,
	}
	tests := []struct {
		name       string
		background bool
		dropAfter  int // first connection ends after this many events
		wantCalls  []string
	}{
		{name: "foreground", dropAfter: len(events), wantCalls: []string{"POST /v1beta/interactions?"}},
		{
			name: "background resumes after drop", background: true, dropAfter: 5,
			wantCalls: []string{
				"POST /v1beta/interactions?",
				"GET /v1beta/interactions/i1?last_event_id=e5&stream=true",
				"DELETE /v1beta/interactions/i1?",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			p := newTestInteractions(t, func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
				send := events[:tt.dropAfter]
				switch r.Method {
				case http.MethodGet:
					send = events[tt.dropAfter:]
				case http.MethodDelete:
					return
				}
				for _, e := range send {
					_, _ = io.WriteString(w, "event: x\ndata: "+e+"\n\n")
				}
				if tt.dropAfter == len(events) || r.Method == http.MethodGet {
					_, _ = io.WriteString(w, "event: done\ndata: [DONE]\n\n")
				}
			})
			s, err := p.ChatStream(context.Background(), chat.Request{
				Model: "m", Messages: []chat.Message{{Role: chat.RoleUser, Content: "q"}},
				ProviderOptions: map[string]any{"gemini": interactionsOptions{Background: tt.background}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var text, reasoning, args strings.Builder
			var last chat.Chunk
			for {
				c, err := s.Next(context.Background())
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				text.WriteString(c.Delta)
				reasoning.WriteString(c.ReasoningDelta)
				for _, d := range c.ToolCallDeltas {
					args.WriteString(d.ArgsDelta)
				}
				last = c
			}
			_ = s.Close()
			if text.String() != "Hi " || reasoning.String() != "hmm" || args.String() != `{"a":1}` {
				t.Errorf("text=%q reasoning=%q args=%q", text.String(), reasoning.String(), args.String())
			}
			if !last.Done || last.FinishReason != "tool_calls" || last.Usage == nil || last.Usage.TotalTokens != 7 {
				t.Errorf("final chunk = %+v", last)
			}
			steps := last.ProviderMetadata["gemini"].(interactionsMessageOptions).InteractionSteps
			wantSteps := []string{
				`{"type":"thought","signature":"sig","summary":[{"type":"text","text":"hmm"}]}`,
				`{"type":"model_output","content":[{"type":"text","text":"Hi "}]}`,
				`{"type":"function_call","id":"c1","name":"f","arguments":{"a":1}}`,
			}
			for i, w := range wantSteps {
				if i >= len(steps) || string(steps[i]) != w {
					t.Errorf("replay steps = %s", steps)
					break
				}
			}
			if !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Errorf("calls = %v, want %v", calls, tt.wantCalls)
			}
		})
	}
}
