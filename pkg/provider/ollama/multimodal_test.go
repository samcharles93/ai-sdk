package ollama

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

// TestChat_Multimodal_ImagePart verifies that an inline image part is
// base64-encoded and routed onto the message's images[] array, with text
// preserved separately as content.
func TestChat_Multimodal_ImagePart(t *testing.T) {
	var got ollamaRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "llava",
			"message": map[string]any{
				"role":    "assistant",
				"content": "I see a cat",
			},
			"done": true,
		})
	}))
	defer srv.Close()

	imgData := []byte{0x89, 0x50, 0x4e, 0x47}
	p := New(Config{BaseURL: srv.URL})
	resp, err := p.Chat(context.Background(), chat.Request{
		Model: "llava",
		Messages: []chat.Message{{
			Role: chat.RoleUser,
			Parts: chat.Parts{
				chat.TextPart{Text: "what's in the picture?"},
				chat.ImagePart{Data: imgData, MediaType: "image/png"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("messages = %d", len(got.Messages))
	}
	m := got.Messages[0]
	if m.Content != "what's in the picture?" {
		t.Errorf("content = %q", m.Content)
	}
	if len(m.Images) != 1 {
		t.Fatalf("images = %d", len(m.Images))
	}
	want := base64.StdEncoding.EncodeToString(imgData)
	if m.Images[0] != want {
		t.Errorf("image[0] = %q, want %q", m.Images[0], want)
	}
	if resp.Content != "I see a cat" {
		t.Errorf("response content = %q", resp.Content)
	}
}

// TestChat_Multimodal_ImagePartURL_Warns verifies that URL-only image
// parts emit a warning rather than being silently dropped.
func TestChat_Multimodal_ImagePartURL_Warns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"role": "assistant", "content": "ok"},
			"done":    true,
		})
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	resp, err := p.Chat(context.Background(), chat.Request{
		Model: "llava",
		Messages: []chat.Message{{
			Role: chat.RoleUser,
			Parts: chat.Parts{
				chat.ImagePart{URL: "https://example.com/cat.png"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.Warnings) == 0 {
		t.Fatalf("expected warning for URL-only ImagePart, got none")
	}
}
