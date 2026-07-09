// runtime-custom-provider demonstrates configuring a custom OpenAI-compatible
// provider via the ai-sdk runtime.
//
// It starts a local mock OpenAI-compatible server, configures a runtime with
// that server as a custom provider, and streams a chat completion through it.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
	"github.com/samcharles93/ai-sdk/runtime"
)

func main() {
	server := httptest.NewServer(http.HandlerFunc(mockHandler))
	defer server.Close()

	// Register the built-in provider classes once at startup.
	runtime.RegisterBuiltinClasses()

	// Configure a custom provider that speaks the OpenAI protocol.
	cfg := runtime.Config{
		Providers: map[string]runtime.ProviderConfig{
			"my-gateway": {
				ID:      "my-gateway",
				Class:   "openai-compatible",
				BaseURL: server.URL,
				Auth: runtime.AuthConfig{
					Type:   runtime.AuthTypeAPIKey,
					APIKey: "dummy-key",
				},
			},
		},
	}

	rt := runtime.NewRuntime(cfg)

	stream, err := rt.ChatStream(context.Background(), "my-gateway/gpt-5.4", core.GenerateOptions{
		Messages: []chat.Message{
			{Role: chat.RoleSystem, Content: "You are a test assistant."},
			{Role: chat.RoleUser, Content: "Say hello."},
		},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("--- streaming ---")
	var text strings.Builder
	for p := range stream.FullStream {
		switch p.Type {
		case core.StreamPartTextDelta:
			text.WriteString(p.TextDelta)
			fmt.Print(p.TextDelta)
		case core.StreamPartFinish:
			fmt.Printf("\n--- finish: %s ---\n", p.FinishReason)
		case core.StreamPartError:
			fmt.Printf("\n--- error: %v ---\n", p.Error)
		}
	}

	usage, err := stream.Usage()
	if err != nil {
		panic(err)
	}
	fmt.Printf("usage: %+v\n", usage)

	if !strings.Contains(text.String(), "custom-provider") {
		panic("expected response from custom provider")
	}
	fmt.Println("custom provider works")
}

func mockHandler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	_ = json.Unmarshal(body, &req)

	w.Header().Set("Content-Type", "text/event-stream")
	flusher := w.(http.Flusher)
	chunks := []string{
		`{"choices":[{"delta":{"role":"assistant"}}]}`,
		`{"choices":[{"delta":{"content":"Hello from "}}]}`,
		`{"choices":[{"delta":{"content":"custom-provider"},"finish_reason":"stop"}]}`,
	}
	for _, ch := range chunks {
		fmt.Fprintf(w, "data: %s\n\n", ch)
		flusher.Flush()
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}
