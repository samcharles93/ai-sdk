package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

// canonicalise re-marshals JSON so map key ordering doesn't affect comparisons.
func canonicalise(t *testing.T, raw string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("canonicalise: %v (raw=%q)", err, raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("canonicalise remarshal: %v", err)
	}
	return string(out)
}

func TestChat_ToolCallNonStream(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
            "candidates":[{
                "content":{"role":"model","parts":[
                    {"functionCall":{"name":"get_weather","args":{"city":"Paris","unit":"c"}}}
                ]},
                "finishReason":"STOP"
            }]
        }`)
	})

	resp, err := p.Chat(context.Background(), chat.Request{
		Model:    "gemini-1.5-flash",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "weather?"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls: got %d want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_0" {
		t.Errorf("ID: got %q want call_0", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("Name: got %q", tc.Name)
	}
	wantArgs := canonicalise(t, `{"city":"Paris","unit":"c"}`)
	gotArgs := canonicalise(t, tc.Arguments)
	if gotArgs != wantArgs {
		t.Errorf("Arguments: got %s want %s", gotArgs, wantArgs)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("FinishReason: got %q want tool_calls", resp.FinishReason)
	}
}

func TestChat_ToolBodyShape(t *testing.T) {
	cases := []struct {
		name      string
		choice    *chat.ToolChoice
		wantMode  string
		wantNames []string
	}{
		{"auto", &chat.ToolChoice{Type: chat.ToolChoiceAuto}, "AUTO", nil},
		{"none", &chat.ToolChoice{Type: chat.ToolChoiceNone}, "NONE", nil},
		{"required", &chat.ToolChoice{Type: chat.ToolChoiceRequired}, "ANY", nil},
		{"named", &chat.ToolChoice{Type: chat.ToolChoiceTool, Name: "get_weather"}, "ANY", []string{"get_weather"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody map[string]any
			p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
			})

			_, err := p.Chat(context.Background(), chat.Request{
				Model:    "m",
				Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
				Tools: []chat.Tool{{
					Name:        "get_weather",
					Description: "Get weather",
					Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
				}},
				ToolChoice: tc.choice,
			})
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}

			tools, _ := gotBody["tools"].([]any)
			if len(tools) != 1 {
				t.Fatalf("tools len: got %d", len(tools))
			}
			decls, _ := tools[0].(map[string]any)["functionDeclarations"].([]any)
			if len(decls) != 1 {
				t.Fatalf("functionDeclarations len: %d", len(decls))
			}
			d := decls[0].(map[string]any)
			if d["name"] != "get_weather" {
				t.Errorf("decl.name: %v", d["name"])
			}
			if _, ok := d["parameters"].(map[string]any); !ok {
				t.Errorf("decl.parameters not an object: %T", d["parameters"])
			}

			toolCfg, _ := gotBody["toolConfig"].(map[string]any)
			if toolCfg == nil {
				t.Fatalf("toolConfig missing")
			}
			fcc, _ := toolCfg["functionCallingConfig"].(map[string]any)
			if fcc["mode"] != tc.wantMode {
				t.Errorf("mode: got %v want %s", fcc["mode"], tc.wantMode)
			}
			if tc.wantNames != nil {
				names, _ := fcc["allowedFunctionNames"].([]any)
				if len(names) != len(tc.wantNames) || names[0] != tc.wantNames[0] {
					t.Errorf("allowedFunctionNames: got %v want %v", names, tc.wantNames)
				}
			} else if _, ok := fcc["allowedFunctionNames"]; ok {
				t.Errorf("allowedFunctionNames should be absent for %s", tc.name)
			}
		})
	}
}

func TestChat_ToolBodyShape_NoChoiceOmitsToolConfig(t *testing.T) {
	var gotBody map[string]any
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
	})
	_, err := p.Chat(context.Background(), chat.Request{
		Model:    "m",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if _, ok := gotBody["toolConfig"]; ok {
		t.Errorf("toolConfig should be omitted when ToolChoice is nil")
	}
}

func TestChat_AssistantToolCallContents(t *testing.T) {
	var gotBody map[string]any
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
	})

	_, err := p.Chat(context.Background(), chat.Request{
		Model: "m",
		Messages: []chat.Message{
			{Role: chat.RoleUser, Content: "weather?"},
			{Role: chat.RoleAssistant, ToolCalls: []chat.ToolCall{{
				ID:        "call_0",
				Name:      "get_weather",
				Arguments: `{"city":"Paris"}`,
			}}},
			{Role: chat.RoleTool, Name: "get_weather", ToolCallID: "call_0", Content: `{"temp":18}`},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	contents, _ := gotBody["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("contents len: %d", len(contents))
	}
	model := contents[1].(map[string]any)
	if model["role"] != "model" {
		t.Errorf("assistant role: got %v want model", model["role"])
	}
	parts := model["parts"].([]any)
	if len(parts) != 1 {
		t.Fatalf("assistant parts len: %d", len(parts))
	}
	fc, ok := parts[0].(map[string]any)["functionCall"].(map[string]any)
	if !ok {
		t.Fatalf("missing functionCall in assistant part: %v", parts[0])
	}
	if fc["name"] != "get_weather" {
		t.Errorf("functionCall.name: %v", fc["name"])
	}
	args, ok := fc["args"].(map[string]any)
	if !ok {
		t.Fatalf("functionCall.args is not an object: %T (%v)", fc["args"], fc["args"])
	}
	if args["city"] != "Paris" {
		t.Errorf("args.city: %v", args["city"])
	}
}

func TestChat_ToolResultContents(t *testing.T) {
	t.Run("json content", func(t *testing.T) {
		var gotBody map[string]any
		p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
		})
		_, err := p.Chat(context.Background(), chat.Request{
			Model: "m",
			Messages: []chat.Message{
				{Role: chat.RoleUser, Content: "q"},
				{Role: chat.RoleTool, Name: "get_weather", Content: `{"temp":18,"unit":"c"}`},
			},
		})
		if err != nil {
			t.Fatalf("Chat: %v", err)
		}
		contents := gotBody["contents"].([]any)
		toolMsg := contents[1].(map[string]any)
		if toolMsg["role"] != "user" {
			t.Errorf("tool result role: got %v want user", toolMsg["role"])
		}
		parts := toolMsg["parts"].([]any)
		fr, ok := parts[0].(map[string]any)["functionResponse"].(map[string]any)
		if !ok {
			t.Fatalf("missing functionResponse")
		}
		if fr["name"] != "get_weather" {
			t.Errorf("name: %v", fr["name"])
		}
		respObj, ok := fr["response"].(map[string]any)
		if !ok {
			t.Fatalf("response not an object: %T", fr["response"])
		}
		if respObj["temp"].(float64) != 18 {
			t.Errorf("response.temp: %v", respObj["temp"])
		}
	})

	t.Run("non-json content wrapped", func(t *testing.T) {
		var gotBody map[string]any
		p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
		})
		_, err := p.Chat(context.Background(), chat.Request{
			Model: "m",
			Messages: []chat.Message{
				{Role: chat.RoleUser, Content: "q"},
				{Role: chat.RoleTool, Name: "echo", Content: `plain text reply`},
			},
		})
		if err != nil {
			t.Fatalf("Chat: %v", err)
		}
		contents := gotBody["contents"].([]any)
		parts := contents[1].(map[string]any)["parts"].([]any)
		fr := parts[0].(map[string]any)["functionResponse"].(map[string]any)
		respObj, ok := fr["response"].(map[string]any)
		if !ok {
			t.Fatalf("response not object")
		}
		if respObj["output"] != "plain text reply" {
			t.Errorf("wrapped output: %v", respObj["output"])
		}
	})

	t.Run("name fallback to tool_call_id", func(t *testing.T) {
		var gotBody map[string]any
		p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
		})
		_, err := p.Chat(context.Background(), chat.Request{
			Model: "m",
			Messages: []chat.Message{
				{Role: chat.RoleUser, Content: "q"},
				{Role: chat.RoleTool, ToolCallID: "fallback_name", Content: `{}`},
			},
		})
		if err != nil {
			t.Fatalf("Chat: %v", err)
		}
		contents := gotBody["contents"].([]any)
		parts := contents[1].(map[string]any)["parts"].([]any)
		fr := parts[0].(map[string]any)["functionResponse"].(map[string]any)
		if fr["name"] != "fallback_name" {
			t.Errorf("name fallback: got %v", fr["name"])
		}
	})
}

func TestChat_ToolChoiceTool_RequiresName(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called")
	})
	_, err := p.Chat(context.Background(), chat.Request{
		Model:      "m",
		Messages:   []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
		ToolChoice: &chat.ToolChoice{Type: chat.ToolChoiceTool},
	})
	if !errors.Is(err, chat.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestChat_AssistantToolCall_InvalidArgsJSON(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called")
	})
	_, err := p.Chat(context.Background(), chat.Request{
		Model: "m",
		Messages: []chat.Message{
			{Role: chat.RoleUser, Content: "hi"},
			{Role: chat.RoleAssistant, ToolCalls: []chat.ToolCall{{
				ID:        "call_0",
				Name:      "x",
				Arguments: "not json{{{",
			}}},
		},
	})
	if !errors.Is(err, chat.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestChatStream_FunctionCallSingle(t *testing.T) {
	chunks := []string{
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"thinking..."}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"Paris"}}}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4,"totalTokenCount":7}}`,
	}

	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			if flusher != nil {
				flusher.Flush()
			}
		}
	})

	stream, err := p.ChatStream(context.Background(), chat.Request{
		Model:    "m",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer stream.Close()

	var allDeltas []chat.ToolCallDelta
	var final chat.Chunk
	var text strings.Builder
	for {
		ch, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		text.WriteString(ch.Delta)
		allDeltas = append(allDeltas, ch.ToolCallDeltas...)
		if ch.Done {
			final = ch
		}
	}

	if text.String() != "thinking..." {
		t.Errorf("text: got %q", text.String())
	}
	if len(allDeltas) != 1 {
		t.Fatalf("tool call deltas: got %d want 1", len(allDeltas))
	}
	if allDeltas[0].Index != 0 || allDeltas[0].ID != "call_0" || allDeltas[0].Name != "get_weather" {
		t.Errorf("delta: %+v", allDeltas[0])
	}
	if canonicalise(t, allDeltas[0].ArgsDelta) != canonicalise(t, `{"city":"Paris"}`) {
		t.Errorf("args: %s", allDeltas[0].ArgsDelta)
	}
	if final.FinishReason != "tool_calls" {
		t.Errorf("FinishReason: got %q want tool_calls", final.FinishReason)
	}
}

func TestChatStream_FunctionCallParallel(t *testing.T) {
	chunks := []string{
		`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"Paris"}}},{"functionCall":{"name":"get_time","args":{"tz":"UTC"}}}]},"finishReason":"STOP"}]}`,
	}

	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			if flusher != nil {
				flusher.Flush()
			}
		}
	})

	stream, err := p.ChatStream(context.Background(), chat.Request{
		Model:    "m",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer stream.Close()

	var allDeltas []chat.ToolCallDelta
	var final chat.Chunk
	for {
		ch, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		allDeltas = append(allDeltas, ch.ToolCallDeltas...)
		if ch.Done {
			final = ch
		}
	}

	if len(allDeltas) != 2 {
		t.Fatalf("deltas: got %d want 2", len(allDeltas))
	}
	if allDeltas[0].Index != 0 || allDeltas[1].Index != 1 {
		t.Errorf("indices: got %d,%d", allDeltas[0].Index, allDeltas[1].Index)
	}
	if final.FinishReason != "tool_calls" {
		t.Errorf("FinishReason: %q", final.FinishReason)
	}

	calls := chat.AssembleToolCalls(allDeltas)
	if len(calls) != 2 {
		t.Fatalf("assembled: got %d want 2", len(calls))
	}
	gotNames := []string{calls[0].Name, calls[1].Name}
	wantNames := []string{"get_weather", "get_time"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Errorf("names: got %v want %v", gotNames, wantNames)
	}
	if calls[0].ID != "call_0" || calls[1].ID != "call_1" {
		t.Errorf("ids: %q,%q", calls[0].ID, calls[1].ID)
	}
}
