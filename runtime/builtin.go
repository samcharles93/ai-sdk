package runtime

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/embed"
	"github.com/samcharles93/ai-sdk/image"
	"github.com/samcharles93/ai-sdk/object"
	"github.com/samcharles93/ai-sdk/provider/anthropic"
	"github.com/samcharles93/ai-sdk/provider/azure"
	"github.com/samcharles93/ai-sdk/provider/cohere"
	"github.com/samcharles93/ai-sdk/provider/deepseek"
	"github.com/samcharles93/ai-sdk/provider/gemini"
	"github.com/samcharles93/ai-sdk/provider/groq"
	"github.com/samcharles93/ai-sdk/provider/mistral"
	"github.com/samcharles93/ai-sdk/provider/ollama"
	"github.com/samcharles93/ai-sdk/provider/openai"
	"github.com/samcharles93/ai-sdk/provider/openaiobject"
	"github.com/samcharles93/ai-sdk/provider/perplexity"
	"github.com/samcharles93/ai-sdk/provider/togetherai"
	"github.com/samcharles93/ai-sdk/provider/xai"
	"github.com/samcharles93/ai-sdk/rerank"
	"github.com/samcharles93/ai-sdk/speech"
	"github.com/samcharles93/ai-sdk/transcribe"
	"github.com/samcharles93/ai-sdk/video"
)

// RegisterBuiltinClasses registers the provider classes and auth resolvers
// shipped with the ai-sdk. Call this once at program startup before
// constructing a Runtime.
func RegisterBuiltinClasses() {
	RegisterAuthResolver(AuthTypeOAuthPKCE, &OAuthPKCEResolver{})

	MustRegisterClass(openAICompatibleClass{})
	MustRegisterClass(anthropicClass())
	MustRegisterClass(azureClass())
	MustRegisterClass(cohereClass())
	MustRegisterClass(deepseekClass())
	MustRegisterClass(geminiClass())
	MustRegisterClass(groqClass())
	MustRegisterClass(mistralClass())
	MustRegisterClass(ollamaClass())
	MustRegisterClass(openaiClass())
	MustRegisterClass(openaiobjectClass())
	MustRegisterClass(perplexityClass())
	MustRegisterClass(togetheraiClass())
	MustRegisterClass(xaiClass())
}

// NPMClassMapping maps models.dev npm package identifiers to the
// provider class names registered by RegisterBuiltinClasses. This lets
// the Runtime select a class automatically for known providers.
//
// Compatibility rules:
//   - "@ai-sdk/google" maps to "gemini" because models.dev publishes Google as
//     provider "google" with npm package "@ai-sdk/google", while the native
//     class in this SDK is registered as "gemini".
var NPMClassMapping = map[string]string{
	"@ai-sdk/openai":            "openai",
	"@ai-sdk/anthropic":         "anthropic",
	"@ai-sdk/azure":             "azure",
	"@ai-sdk/cohere":            "cohere",
	"@ai-sdk/deepseek":          "deepseek",
	"@ai-sdk/gemini":            "gemini",
	"@ai-sdk/google":            "gemini",
	"@ai-sdk/groq":              "groq",
	"@ai-sdk/mistral":           "mistral",
	"@ai-sdk/ollama":            "ollama",
	"@ai-sdk/perplexity":        "perplexity",
	"@ai-sdk/togetherai":        "togetherai",
	"@ai-sdk/xai":               "xai",
	"@ai-sdk/openai-compatible": "openai-compatible",
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
	name            string
	caps            []Capability
	buildChat       func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error)
	buildImage      func(apiKey, baseURL string, httpClient *http.Client) (image.Provider, error)
	buildVideo      func(apiKey, baseURL string, httpClient *http.Client) (video.Provider, error)
	buildObject     func(apiKey, baseURL string, httpClient *http.Client) (object.Provider, error)
	buildRerank     func(apiKey, baseURL string, httpClient *http.Client) (rerank.Provider, error)
	buildSpeech     func(apiKey, baseURL string, httpClient *http.Client) (speech.Provider, error)
	buildTranscribe func(apiKey, baseURL string, httpClient *http.Client) (transcribe.Provider, error)
	build           func(apiKey, baseURL string, httpClient *http.Client) (providerSetBuilder, error)
}

// providerSetBuilder is satisfied by concrete providers that implement
// multiple domain interfaces (chat + embed, etc.).
type providerSetBuilder interface {
	chat.Provider
	embed.Provider
}

func (c simpleClass) Name() string { return c.name }

func (c simpleClass) Supports(cap Capability) bool {
	return slices.Contains(c.caps, cap)
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
		// A provider built through the combined builder may also satisfy
		// generation interfaces (image/video/object/rerank). Surface them
		// only when the class advertises the capability, so a nil assertion
		// is simply skipped rather than producing a half-wired set.
		if c.Supports(CapabilityImage) {
			if img, ok := p.(image.Provider); ok {
				set.Image = img
			}
		}
		if c.Supports(CapabilityVideo) {
			if vid, ok := p.(video.Provider); ok {
				set.Video = vid
			}
		}
		if c.Supports(CapabilityObject) {
			if obj, ok := p.(object.Provider); ok {
				set.Object = obj
			}
		}
		if c.Supports(CapabilityRerank) {
			if rk, ok := p.(rerank.Provider); ok {
				set.Rerank = rk
			}
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
	if c.buildImage != nil {
		p, err := c.buildImage(apiKey, baseURL, httpClient)
		if err != nil {
			return ProviderSet{}, fmt.Errorf("runtime/%s: %w", c.name, err)
		}
		set.Image = p
	}
	if c.buildVideo != nil {
		p, err := c.buildVideo(apiKey, baseURL, httpClient)
		if err != nil {
			return ProviderSet{}, fmt.Errorf("runtime/%s: %w", c.name, err)
		}
		set.Video = p
	}
	if c.buildObject != nil {
		p, err := c.buildObject(apiKey, baseURL, httpClient)
		if err != nil {
			return ProviderSet{}, fmt.Errorf("runtime/%s: %w", c.name, err)
		}
		set.Object = p
	}
	if c.buildRerank != nil {
		p, err := c.buildRerank(apiKey, baseURL, httpClient)
		if err != nil {
			return ProviderSet{}, fmt.Errorf("runtime/%s: %w", c.name, err)
		}
		set.Rerank = p
	}
	if c.buildSpeech != nil {
		p, err := c.buildSpeech(apiKey, baseURL, httpClient)
		if err != nil {
			return ProviderSet{}, fmt.Errorf("runtime/%s: %w", c.name, err)
		}
		set.Speech = p
	}
	if c.buildTranscribe != nil {
		p, err := c.buildTranscribe(apiKey, baseURL, httpClient)
		if err != nil {
			return ProviderSet{}, fmt.Errorf("runtime/%s: %w", c.name, err)
		}
		set.Transcribe = p
	}
	return set, nil
}

func openaiClass() ProviderClass {
	return simpleClass{
		name: "openai",
		caps: []Capability{CapabilityChat, CapabilitySpeech, CapabilityTranscribe},
		buildChat: func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error) {
			return openai.New(openai.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
		buildSpeech: func(apiKey, baseURL string, httpClient *http.Client) (speech.Provider, error) {
			return openai.New(openai.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
		buildTranscribe: func(apiKey, baseURL string, httpClient *http.Client) (transcribe.Provider, error) {
			return openai.New(openai.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
	}
}

func openaiobjectClass() ProviderClass {
	return simpleClass{
		name: "openaiobject",
		caps: []Capability{CapabilityObject},
		buildObject: func(apiKey, baseURL string, httpClient *http.Client) (object.Provider, error) {
			return openaiobject.New(openaiobject.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
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
		caps: []Capability{CapabilityChat, CapabilityEmbed, CapabilityImage},
		build: func(apiKey, baseURL string, httpClient *http.Client) (providerSetBuilder, error) {
			return azure.New(azure.Config{APIKey: apiKey, Endpoint: baseURL, HTTPClient: httpClient})
		},
	}
}

func cohereClass() ProviderClass {
	return simpleClass{
		name: "cohere",
		caps: []Capability{CapabilityChat, CapabilityEmbed, CapabilityRerank},
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
		caps: []Capability{CapabilityChat, CapabilityTranscribe},
		buildChat: func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error) {
			return groq.New(groq.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
		buildTranscribe: func(apiKey, baseURL string, httpClient *http.Client) (transcribe.Provider, error) {
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
		caps: []Capability{CapabilityChat, CapabilityImage, CapabilityVideo},
		buildChat: func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error) {
			return xai.New(xai.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
		buildImage: func(apiKey, baseURL string, httpClient *http.Client) (image.Provider, error) {
			return xai.New(xai.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
		buildVideo: func(apiKey, baseURL string, httpClient *http.Client) (video.Provider, error) {
			return xai.New(xai.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
	}
}

func togetheraiClass() ProviderClass {
	return simpleClass{
		name: "togetherai",
		caps: []Capability{CapabilityChat, CapabilityImage, CapabilityRerank},
		buildChat: func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error) {
			return openai.New(openai.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
		buildImage: func(apiKey, baseURL string, httpClient *http.Client) (image.Provider, error) {
			return togetherai.New(togetherai.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
		buildRerank: func(apiKey, baseURL string, httpClient *http.Client) (rerank.Provider, error) {
			return togetherai.New(togetherai.Config{APIKey: apiKey, BaseURL: baseURL, HTTPClient: httpClient})
		},
	}
}

func (cfg ProviderConfig) httpClient() *http.Client {
	if cfg.Timeout <= 0 {
		return &http.Client{}
	}
	return &http.Client{Timeout: time.Duration(cfg.Timeout) * time.Millisecond}
}
