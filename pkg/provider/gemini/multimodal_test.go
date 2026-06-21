package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

// TestChat_Multimodal_InlineData verifies that an inline ImagePart is
// base64-encoded and serialised as a contents[].parts[].inline_data
// wire entry alongside any text parts.
func TestChat_Multimodal_InlineData(t *testing.T) {
	var bodyBytes []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []any{map[string]any{
				"content": map[string]any{
					"role":  "model",
					"parts": []any{map[string]any{"text": "I see it"}},
				},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]any{},
		})
	}))
	defer srv.Close()

	imgData := []byte{0x89, 0x50, 0x4e, 0x47}
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Chat(context.Background(), chat.Request{
		Model: "gemini-2.0-flash",
		Messages: []chat.Message{{
			Role: chat.RoleUser,
			Parts: chat.Parts{
				chat.TextPart{Text: "describe"},
				chat.ImagePart{Data: imgData, MediaType: "image/png"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var parsed struct {
		Contents []struct {
			Parts []struct {
				Text       string `json:"text,omitempty"`
				InlineData *struct {
					MimeType string `json:"mimeType"`
					Data     string `json:"data"`
				} `json:"inlineData,omitempty"`
			} `json:"parts"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		t.Fatalf("decode wire body: %v\n%s", err, bodyBytes)
	}
	if len(parsed.Contents) != 1 {
		t.Fatalf("contents = %d", len(parsed.Contents))
	}
	parts := parsed.Contents[0].Parts
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2", len(parts))
	}
	if parts[0].Text != "describe" {
		t.Errorf("parts[0].text = %q", parts[0].Text)
	}
	if parts[1].InlineData == nil {
		t.Fatalf("parts[1].inline_data is nil")
	}
	if parts[1].InlineData.MimeType != "image/png" {
		t.Errorf("mimeType = %q", parts[1].InlineData.MimeType)
	}
	want := base64.StdEncoding.EncodeToString(imgData)
	if parts[1].InlineData.Data != want {
		t.Errorf("inline_data.data = %q, want %q", parts[1].InlineData.Data, want)
	}
}
