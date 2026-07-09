// runtime-custom-class demonstrates registering a custom provider class
// with the ai-sdk runtime.
//
// A custom class lets users plug in providers that need behavior beyond
// the built-in "openai-compatible" class — for example, a provider that
// discovers models from an internal gateway or performs a bespoke auth
// handshake before returning a chat.Provider.
package main

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
	"github.com/samcharles93/ai-sdk/runtime"
)

// echoProvider is a trivial chat.Provider that echoes the last user
// message. It stands in for any custom backend.
type echoProvider struct{}

func (echoProvider) Name() string { return "echo" }
func (echoProvider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	return chat.Response{
		Role:    chat.RoleAssistant,
		Content: "echo: " + lastUserContent(req.Messages),
		Usage:   chat.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
	}, nil
}

func (echoProvider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	return &echoStream{content: "echo: " + lastUserContent(req.Messages)}, nil
}

type echoStream struct {
	content string
	sent    bool
}

func (s *echoStream) Next(ctx context.Context) (chat.Chunk, error) {
	if !s.sent {
		s.sent = true
		return chat.Chunk{Delta: s.content}, nil
	}
	return chat.Chunk{Done: true}, nil
}
func (s *echoStream) Close() error { return nil }

func lastUserContent(msgs []chat.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == chat.RoleUser {
			return msgs[i].Text()
		}
	}
	return ""
}

// echoClass is the runtime-facing factory for the echo provider.
type echoClass struct{}

func (echoClass) Name() string { return "echo" }
func (echoClass) Supports(cap runtime.Capability) bool {
	return cap == runtime.CapabilityChat
}

func (echoClass) New(ctx context.Context, cfg runtime.ProviderConfig, model runtime.ModelInfo) (runtime.ProviderSet, error) {
	return runtime.ProviderSet{Chat: echoProvider{}}, nil
}

func main() {
	runtime.RegisterBuiltinClasses()
	runtime.RegisterClass(echoClass{})

	cfg := runtime.Config{
		Providers: map[string]runtime.ProviderConfig{
			"echo": {
				ID:    "echo",
				Class: "echo",
			},
		},
	}

	rt := runtime.NewRuntime(cfg)

	result, err := rt.Chat(context.Background(), "echo/default", core.GenerateOptions{
		Messages: []chat.Message{
			{Role: chat.RoleUser, Content: "hello world"},
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Text)
	fmt.Printf("usage: %+v\n", result.TotalUsage)
}
