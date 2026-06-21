package runtime

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/samcharles93/ai-sdk/pkg/chat"
	"github.com/samcharles93/ai-sdk/pkg/embed"
	"github.com/samcharles93/ai-sdk/pkg/provider/anthropic"
	"github.com/samcharles93/ai-sdk/pkg/provider/azure"
	"github.com/samcharles93/ai-sdk/pkg/provider/cohere"
	"github.com/samcharles93/ai-sdk/pkg/provider/deepseek"
	"github.com/samcharles93/ai-sdk/pkg/provider/gemini"
	"github.com/samcharles93/ai-sdk/pkg/provider/groq"
	"github.com/samcharles93/ai-sdk/pkg/provider/mistral"
	"github.com/samcharles93/ai-sdk/pkg/provider/ollama"
	"github.com/samcharles93/ai-sdk/pkg/provider/openai"
	"github.com/samcharles93/ai-sdk/pkg/provider/perplexity"
	"github.com/samcharles93/ai-sdk/pkg/provider/xai"
)

// RegisterBuiltinClasses registers the provider classes and auth resolvers
// shipped with the ai-sdk. Call this once at program startup before
// constructing a Runtime.
func RegisterBuiltinClasses() {
	RegisterAuthResolver(AuthTypeOAuthPKCE, &OAuthPKCEResolver{})

	MustRegisterClass(openAICompatibleClass{})
	MustRegisterClass(anthropicClass())
	MustRegisterClass(anthropicClass())
	MustRegisterClass(azureClass())
	MustRegisterClass(cohereClass())
	MustRegisterClass(deepseekClass())
	MustRegisterClass(geminiClass())
	MustRegisterClass(groqClass())
	MustRegisterClass(mistralClass())
	MustRegisterClass(ollamaClass())
	MustRegisterClass(openaiClass())
	MustRegisterClass(perplexityClass())
	MustRegisterClass(xaiClass())
}

// NPMClassMapping maps models.dev npm package identifiers to the
// provider class names registered by RegisterBuiltinClasses. This lets
// the Runtime select a class automatically for known providers.
var NPMClassMapping = map[string]string{
	"@ai-sdk/openai":           "openai",
	"@ai-sdk/anthropic":          "anthropic",
	"@ai-sdk/azure":              "azure",
	"@ai-sdk/cohere":             "cohere",
	"@ai-sdk/deepseek":           "deepseek",
	"@ai-sdk/gemini":             "gemini",
	"@ai-sdk/groq":               "groq",
	"@ai-sdk/mistral":            "mistral",
	"@ai-sdk/ollama":             "ollama",
	"@ai-sdk/perplexity":         "perplexity",
	"@ai-sdk/xai":                "xai",
	"@ai-sdk/openai-compatible":  "openai-compatible",
}

// openAICompatibleClass is the generic class for any endpoint that speaks
// the OpenAI chat completions protocol. It is used both for explicit
// openai-compatible providers and as a fallback for unknown npm packages.
type openAICompatibleClass struct{}

func (openAICompatibleClass) Name() string { return "openai-compatible" }

func (openAICompatibleClass) Supports(cap Capability) bool {
	switch cap {
	case CapabilityChat:
		return true
	default:
		return false
	}
}

func (openAICompatibleClass) New(ctx context.Context, cfg ProviderConfig, model ModelInfo) (ProviderSet, error) {
	auth, err := resolveAuth(ctx, cfg)
	if err != nil {
		return ProviderSet{}, err
	}
	apiKey := auth.Token
	if strings.TrimSpace(apiKey) == "" && cfg.Auth.Type != AuthTypeNone {
		return ProviderSet{}, fmt.Errorf("runtime/%s: no bearer token resolved for provider %q", cfg.Class, cfg.ID)
	}
	httpClient := cfg.httpClient()
	p, err := openai.New(openai.Config{
		APIKey:     apiKey,
		BaseURL:    model.providerURL(cfg.BaseURL),
		HTTPClient: httpClient,
	})
	if err != nil {
		return ProviderSet{}, fmt.Errorf("runtime/%s: %w", cfg.Class, err)
	}
	return ProviderSet{Chat: p}, nil
}

// simpleClass wraps a provider constructor that returns a value
// implementing one or more domain interfaces.
type simpleClass struct {
	name      string
	caps      []Capability
	buildChat func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error)
	build     func(apiKey, baseURL string, httpClient *http.Client) (providerSetBuilder, error)
}

// providerSetBuilder is satisfied by concrete providers that implement
// multiple domain interfaces (chat + embed, etc.).
type providerSetBuilder interface {
	chat.Provider
	embed.Provider
}

func (c simpleClass) Name() string { return c.name }

func (c simpleClass) Supports(cap Capability) bool {
	for _, supported := range c.caps {
		if supported == cap {
			return true
		}
	}
	return false
}

func (c simpleClass) New(ctx context.Context, cfg ProviderConfig, model ModelInfo) (ProviderSet, error) {
	auth, err := resolveAuth(ctx, cfg)
	if err != nil {
		return ProviderSet{}, err
	}
	apiKey := auth.Token
	baseURL := model.providerURL(cfg.BaseURL)
	httpClient := cfg.httpClient()
	set := ProviderSet{}
	if c.build != nil {
		p, err := c.build(apiKey, baseURL, httpClient)
		if err != nil {
			return ProviderSet{}, fmt.Errorf("runtime/%s: %w", c.name, err)
		}
		if c.Supports(CapabilityChat) {
			set.Chat = p
		}
		if c.Supports(CapabilityEmbed) {
			set.Embed = p
		}
		return set, nil
	}
	if c.buildChat != nil {
		p, err := c.buildChat(apiKey, baseURL, httpClient)
		if err != nil {
			return ProviderSet{}, fmt.Errorf("runtime/%s: %w", c.name, err)
		}
		set.Chat = p
	}
	return set, nil
}

func openaiClass() ProviderClass {
	return simpleClass{
		name: "openai",
		caps: []Capability{CapabilityChat},
		buildChat: func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error) {
			return openai.New(openai.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
	}
}

func anthropicClass() ProviderClass {
	return simpleClass{
		name: "anthropic",
		caps: []Capability{CapabilityChat},
		buildChat: func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error) {
			return anthropic.New(anthropic.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
	}
}

func azureClass() ProviderClass {
	return simpleClass{
		name: "azure",
		caps: []Capability{CapabilityChat, CapabilityEmbed},
		build: func(apiKey, baseURL string, httpClient *http.Client) (providerSetBuilder, error) {
			return azure.New(azure.Config{APIKey: apiKey, Endpoint: baseURL, HTTPClient: httpClient})
		},
	}
}

func cohereClass() ProviderClass {
	return simpleClass{
		name: "cohere",
		caps: []Capability{CapabilityChat, CapabilityEmbed},
		build: func(apiKey, baseURL string, httpClient *http.Client) (providerSetBuilder, error) {
			return cohere.New(cohere.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
	}
}

func deepseekClass() ProviderClass {
	return simpleClass{
		name: "deepseek",
		caps: []Capability{CapabilityChat},
		buildChat: func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error) {
			return deepseek.New(deepseek.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
	}
}

func geminiClass() ProviderClass {
	return simpleClass{
		name: "gemini",
		caps: []Capability{CapabilityChat, CapabilityEmbed},
		build: func(apiKey, baseURL string, httpClient *http.Client) (providerSetBuilder, error) {
			return gemini.New(gemini.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
	}
}

func groqClass() ProviderClass {
	return simpleClass{
		name: "groq",
		caps: []Capability{CapabilityChat},
		buildChat: func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error) {
			return groq.New(groq.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
	}
}

func mistralClass() ProviderClass {
	return simpleClass{
		name: "mistral",
		caps: []Capability{CapabilityChat, CapabilityEmbed},
		build: func(apiKey, baseURL string, httpClient *http.Client) (providerSetBuilder, error) {
			return mistral.New(mistral.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
	}
}

func ollamaClass() ProviderClass {
	return simpleClass{
		name: "ollama",
		caps: []Capability{CapabilityChat, CapabilityEmbed},
		build: func(apiKey, baseURL string, httpClient *http.Client) (providerSetBuilder, error) {
			return ollama.New(ollama.Config{BaseURL: baseURL, HTTPClient: httpClient}), nil
		},
	}
}

func perplexityClass() ProviderClass {
	return simpleClass{
		name: "perplexity",
		caps: []Capability{CapabilityChat},
		buildChat: func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error) {
			return perplexity.New(perplexity.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
	}
}

func xaiClass() ProviderClass {
	return simpleClass{
		name: "xai",
		caps: []Capability{CapabilityChat},
		buildChat: func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error) {
			return xai.New(xai.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
	}
}

func (cfg ProviderConfig) httpClient() *http.Client {
	timeout := time.Duration(cfg.Timeout) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &http.Client{Timeout: timeout}
}
