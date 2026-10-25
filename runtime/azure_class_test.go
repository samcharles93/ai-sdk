package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

func TestAzureClassResolvesDeploymentAndAPIVersion(t *testing.T) {
	// The deployment name and api-version are Azure query parameters the
	// shared builder cannot supply; without them azure.New fails on every
	// runtime call. A reasoning deployment needs the api-version override.
	tests := []struct {
		name    string
		options map[string]any
		model   ModelInfo
		want    string
	}{
		{
			name:  "deployment defaults to the model",
			model: ModelInfo{ID: "gpt-4o"},
			want:  "/openai/deployments/gpt-4o/chat/completions?api-version=2024-02-01",
		},
		{
			name:    "provider options override both",
			options: map[string]any{"deployment": "my-dep", "api_version": "2024-09-01-preview"},
			model:   ModelInfo{ID: "o3-mini"},
			want:    "/openai/deployments/my-dep/chat/completions?api-version=2024-09-01-preview",
		},
		{
			name:    "model extra beats provider options",
			options: map[string]any{"deployment": "provider-dep"},
			model:   ModelInfo{ID: "o3-mini", Extra: map[string]any{"deployment": "model-dep"}},
			want:    "/openai/deployments/model-dep/chat/completions?api-version=2024-02-01",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.String()
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
			}))
			defer srv.Close()

			set, err := (azureClass{}).New(context.Background(), ProviderConfig{
				ID:      "az",
				Class:   "azure",
				BaseURL: srv.URL,
				Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "k"},
				Options: tc.options,
			}, tc.model)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := set.Chat.Chat(context.Background(), chat.Request{
				Model:    tc.model.ID,
				Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
			}); err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if gotPath != tc.want {
				t.Fatalf("path = %q, want %q", gotPath, tc.want)
			}
		})
	}
}
