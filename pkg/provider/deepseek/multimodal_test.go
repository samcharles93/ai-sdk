package deepseek

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

// TestChat_Multimodal_ImagePart_Warns verifies that an ImagePart in a
// DeepSeek request produces a warning (current models are text-only)
// while text parts are preserved.
func TestChat_Multimodal_ImagePart_Warns(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "x",
			"model": "deepseek-chat",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{},
		})
	}))
	defer srv.Close()

	p, nerr := New(Config{APIKey: "k", BaseURL: srv.URL})
	if nerr != nil {
		t.Fatalf("New: %v", nerr)
	}
	resp, err := p.Chat(context.Background(), chat.Request{
		Model: "deepseek-chat",
		Messages: []chat.Message{{
			Role: chat.RoleUser,
			Parts: chat.Parts{
				chat.TextPart{Text: "hello"},
				chat.ImagePart{Data: []byte{1, 2, 3}, MediaType: "image/png"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.Warnings) == 0 {
		t.Fatalf("expected warning for ImagePart on deepseek, got none")
	}
	// content should be a plain string of joined text parts
	msgs, _ := got["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %d", len(msgs))
	}
	m := msgs[0].(map[string]any)
	c, _ := m["content"].(string)
	if !strings.Contains(c, "hello") {
		t.Errorf("content = %q, expected to contain text", c)
	}
}

// TestChat_Reasoning_Content verifies that DeepSeek's reasoning_content
// response field is decoded into a ReasoningPart.
func TestChat_Reasoning_Content(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "x",
			"model": "deepseek-reasoner",
			"choices": []any{map[string]any{
				"message": map[string]any{
					"role":              "assistant",
					"content":           "the answer is 42",
					"reasoning_content": "let me think...",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{},
		})
	}))
	defer srv.Close()

	p, nerr := New(Config{APIKey: "k", BaseURL: srv.URL})
	if nerr != nil {
		t.Fatalf("New: %v", nerr)
	}
	resp, err := p.Chat(context.Background(), chat.Request{
		Model:    "deepseek-reasoner",
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "compute"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.Parts) != 2 {
		t.Fatalf("parts = %d, want 2", len(resp.Parts))
	}
	rp, ok := resp.Parts[0].(chat.ReasoningPart)
	if !ok {
		t.Fatalf("parts[0] = %T, want ReasoningPart", resp.Parts[0])
	}
	if rp.Text != "let me think..." {
		t.Errorf("reasoning text = %q", rp.Text)
	}
	tp, ok := resp.Parts[1].(chat.TextPart)
	if !ok {
		t.Fatalf("parts[1] = %T, want TextPart", resp.Parts[1])
	}
	if tp.Text != "the answer is 42" {
		t.Errorf("text = %q", tp.Text)
	}
}
