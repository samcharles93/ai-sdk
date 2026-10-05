package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
)

// capturingChatProvider records the request it receives so tests can assert
// what the runtime put on it.
type capturingChatProvider struct {
	last chat.Request
}

func (p *capturingChatProvider) Name() string { return "capturing" }

func (p *capturingChatProvider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	p.last = req
	return chat.Response{Role: chat.RoleAssistant, Content: "ok"}, nil
}

func (p *capturingChatProvider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	p.last = req
	return emptyStream{}, nil
}

// emptyStream is an immediately exhausted stream for the capturing provider.
type emptyStream struct{}

func (emptyStream) Next(context.Context) (chat.Chunk, error) { return chat.Chunk{}, io.EOF }
func (emptyStream) Close() error                             { return nil }

type capturingClass struct {
	className string
	provider  *capturingChatProvider
}

func (c capturingClass) Name() string { return c.className }

func (c capturingClass) Supports(cap Capability) bool { return cap == CapabilityChat }

func (c capturingClass) New(context.Context, ProviderConfig, ModelInfo) (ProviderSet, error) {
	return ProviderSet{Chat: c.provider}, nil
}

// TestRuntimeChatCarriesModelInfo guards the plumbing that carries resolved
// model facts (reasoning, temperature support) onto the chat request. Without
// it a provider has nothing to decide on and sends max_tokens/temperature to
// models that reject them.
func TestRuntimeChatCarriesModelInfo(t *testing.T) {
	RegisterBuiltinClasses()
	provider := &capturingChatProvider{}
	RegisterClass(capturingClass{className: "capturing-class-chat", provider: provider})

	noTemperature := false
	rt := NewRuntime(Config{
		Providers: map[string]ProviderConfig{
			"capture": {
				ID:    "capture",
				Class: "capturing-class-chat",
				Models: []ModelConfig{{
					ID:          "reasoner",
					Reasoning:   true,
					Temperature: &noTemperature,
				}},
			},
		},
	})

	if _, err := rt.Chat(context.Background(), "capture/reasoner", core.GenerateOptions{
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if provider.last.Model != "reasoner" {
		t.Errorf("request Model = %q, want reasoner", provider.last.Model)
	}
	if !provider.last.ModelInfo.Reasoning {
		t.Error("request ModelInfo.Reasoning = false, want true")
	}
	if provider.last.ModelInfo.Temperature == nil || *provider.last.ModelInfo.Temperature {
		t.Errorf("request ModelInfo.Temperature = %v, want pointer to false", provider.last.ModelInfo.Temperature)
	}
}

// TestRuntimeChatStreamCarriesModelInfo covers the streaming entry point.
func TestRuntimeChatStreamCarriesModelInfo(t *testing.T) {
	RegisterBuiltinClasses()
	provider := &capturingChatProvider{}
	RegisterClass(capturingClass{className: "capturing-class-stream", provider: provider})

	temperature := true
	rt := NewRuntime(Config{
		Providers: map[string]ProviderConfig{
			"capture": {
				ID:    "capture",
				Class: "capturing-class-stream",
				Models: []ModelConfig{{
					ID:          "classic",
					Reasoning:   false,
					Temperature: &temperature,
				}},
			},
		},
	})

	stream, err := rt.ChatStream(context.Background(), "capture/classic", core.GenerateOptions{
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	for range stream.FullStream {
	}
	if provider.last.ModelInfo.Reasoning {
		t.Error("request ModelInfo.Reasoning = true, want false")
	}
	if provider.last.ModelInfo.Temperature == nil || !*provider.last.ModelInfo.Temperature {
		t.Errorf("request ModelInfo.Temperature = %v, want pointer to true", provider.last.ModelInfo.Temperature)
	}
}

// TestRuntimeCatalogModelInfoReachesRequest verifies that a model flag coming
// from catalog metadata (not just provider config) reaches the request. This is
// the GPT-6/future-model path: catalog reasoning=true, temperature=false.
func TestRuntimeCatalogModelInfoReachesRequest(t *testing.T) {
	RegisterBuiltinClasses()
	provider := &capturingChatProvider{}
	RegisterClass(capturingClass{className: "capturing-class-catalog", provider: provider})

	noTemperature := false
	cat := NewCatalog(CatalogOptions{})
	cat.MergeProviders(map[string]CatalogProvider{
		"demo": {
			ID: "demo",
			Models: map[string]CatalogModel{
				"gpt-6-demo": {ID: "gpt-6-demo", Reasoning: true, Temperature: &noTemperature},
			},
		},
	})

	rt := NewRuntimeWithCatalog(Config{
		Providers: map[string]ProviderConfig{
			"demo": {ID: "demo", Class: "capturing-class-catalog"},
		},
	}, cat)

	if _, err := rt.Chat(context.Background(), "demo/gpt-6-demo", core.GenerateOptions{
		Messages: []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	info := provider.last.ModelInfo
	if !info.Reasoning {
		t.Error("request ModelInfo.Reasoning = false, want true")
	}
	if info.Temperature == nil || *info.Temperature {
		t.Errorf("request ModelInfo.Temperature = %v, want pointer to false", info.Temperature)
	}
}

// TestChatCompletionsDialectIsClassScoped pins the wire shape produced by the
// same resolved model flags across provider classes. Only OpenAI's own API may
// turn a reasoning flag into max_completion_tokens and drop a temperature the
// model rejects; an OpenAI-compatible server (the generic class, but also the
// openai transport used by the togetherai and minimax classes) either ignores
// max_completion_tokens — silently losing the output bound — or rejects it with
// a 400, and keeps temperature exactly as the caller set it.
func TestChatCompletionsDialectIsClassScoped(t *testing.T) {
	RegisterBuiltinClasses()

	tests := []struct {
		class           string
		wantMaxKey      string
		wantTemperature bool
	}{
		{class: "openai", wantMaxKey: "max_completion_tokens", wantTemperature: false},
		{class: "openai-compatible", wantMaxKey: "max_tokens", wantTemperature: true},
		{class: "togetherai", wantMaxKey: "max_tokens", wantTemperature: true},
		{class: "minimax", wantMaxKey: "max_tokens", wantTemperature: true},
	}

	for _, tt := range tests {
		t.Run(tt.class, func(t *testing.T) {
			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"x","model":"reasoner","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			}))
			defer srv.Close()

			noTemperature := false
			rt := NewRuntime(Config{
				Providers: map[string]ProviderConfig{
					"p": {
						ID:      "p",
						Class:   tt.class,
						BaseURL: srv.URL,
						Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "k"},
						Models: []ModelConfig{{
							ID:          "reasoner",
							Reasoning:   true,
							Temperature: &noTemperature,
						}},
					},
				},
			})

			if _, err := rt.Chat(context.Background(), "p/reasoner", core.GenerateOptions{
				Messages:    []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
				MaxTokens:   64,
				Temperature: 0.7,
			}); err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if gotBody[tt.wantMaxKey] != float64(64) {
				t.Errorf("%s = %v, want 64; body=%v", tt.wantMaxKey, gotBody[tt.wantMaxKey], gotBody)
			}
			otherKey := "max_tokens"
			if tt.wantMaxKey == "max_tokens" {
				otherKey = "max_completion_tokens"
			}
			if _, ok := gotBody[otherKey]; ok {
				t.Errorf("body unexpectedly carries %s: %v", otherKey, gotBody)
			}
			if _, ok := gotBody["temperature"]; ok != tt.wantTemperature {
				t.Errorf("temperature present = %v, want %v; body=%v", ok, tt.wantTemperature, gotBody)
			}
		})
	}
}
