package runtime

import (
	"context"
	"strings"
	"sync"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/embed"
)

// Capability names a model capability a ProviderClass may satisfy.
type Capability string

// Standard capabilities.
const (
	CapabilityChat  Capability = "chat"
	CapabilityEmbed Capability = "embed"
)

// ProviderConfig is the minimal information needed to construct a
// provider instance. The Class field selects which ProviderClass factory
// is invoked.
type ProviderConfig struct {
	// ID is the local identifier for this provider instance (e.g. "openai",
	// "my-maas"). It must be unique within a Runtime.
	ID string

	// Class selects the registered ProviderClass factory. Examples:
	// "openai-compatible", "anthropic-messages".
	Class string

	// BaseURL is the provider's API base URL. Individual models may
	// override this via their own URL field.
	BaseURL string

	// Auth describes how to obtain credentials for this provider.
	Auth AuthConfig

	// Headers are extra HTTP headers merged into every request.
	Headers map[string]string

	// Insecure disables TLS certificate verification.
	Insecure bool

	// Timeout for provider HTTP requests in milliseconds. Zero means the
	// class default.
	Timeout int

	// Options carries class-specific options as a generic map. Classes
	// read only the keys they understand.
	Options map[string]any

	// Models allows per-provider model overrides/advertisements. These
	// augment or override the catalog when the runtime resolves model
	// references.
	Models []ModelConfig
}

// ModelConfig is a configured model entry for a provider. It is merged
// with catalog metadata by the runtime.
type ModelConfig struct {
	ID               string         `json:"id"`
	Name             string         `json:"name,omitempty"`
	URL              string         `json:"url,omitempty"`
	ContextWindow    int            `json:"context_window,omitempty"`
	MaxOutputTokens  int            `json:"max_output_tokens,omitempty"`
	Reasoning        bool           `json:"reasoning,omitempty"`
	ToolCall         bool           `json:"tool_call,omitempty"`
	StructuredOutput bool           `json:"structured_output,omitempty"`
	Temperature      bool           `json:"temperature,omitempty"`
	Cost             CostConfig     `json:"cost"`
	Extra            map[string]any `json:"extra,omitempty"`
}

// CostConfig carries per-token pricing metadata.
type CostConfig struct {
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

// ModelInfo is the runtime's normalised view of a model, combining
// catalog data and configured overrides.
type ModelInfo struct {
	ID               string
	ProviderID       string
	Name             string
	URL              string
	ContextWindow    int
	MaxOutputTokens  int
	Reasoning        bool
	ReasoningOptions []ReasoningOption
	ToolCall         bool
	StructuredOutput bool
	Temperature      bool
	Cost             CostConfig
	Extra            map[string]any
}

// providerURL returns the most specific URL known for the model.
func (m ModelInfo) providerURL(baseURL string) string {
	if strings.TrimSpace(m.URL) != "" {
		return strings.TrimRight(m.URL, "/")
	}
	if strings.TrimSpace(baseURL) != "" {
		return strings.TrimRight(baseURL, "/")
	}
	return ""
}

// ProviderSet is a collection of domain providers produced by a single
// ProviderClass instance.
type ProviderSet struct {
	Chat  chat.Provider
	Embed embed.Provider
}

// Has reports whether the set satisfies cap.
func (s ProviderSet) Has(cap Capability) bool {
	switch cap {
	case CapabilityChat:
		return s.Chat != nil
	case CapabilityEmbed:
		return s.Embed != nil
	default:
		return false
	}
}

// ProviderClass is a factory for provider instances. Each class knows how
// to turn a ProviderConfig (base URL, auth, options) and a resolved model
// into one or more domain providers.
type ProviderClass interface {
	// Name returns the stable class identifier used in ProviderConfig.Class.
	Name() string

	// Supports reports whether this class can satisfy cap.
	Supports(cap Capability) bool

	// New builds a ProviderSet from cfg for the given model. ctx may be
	// used for short-lived setup requests (e.g. discovery or token
	// exchange), but must not be stored.
	New(ctx context.Context, cfg ProviderConfig, model ModelInfo) (ProviderSet, error)
}

// ClassRegistry manages provider class registrations.
//
// It is kept as a package-level registry so that built-in classes and
// custom classes can be registered once at program startup without needing
// to thread a registry instance through every caller.
var (
	classMu       sync.RWMutex
	classRegistry = make(map[string]ProviderClass)
)

// RegisterClass registers a provider class. Calling RegisterClass with the
// same name twice is a no-op (the first registration wins).
func RegisterClass(c ProviderClass) {
	if c == nil {
		panic("runtime: RegisterClass called with nil ProviderClass")
	}
	name := c.Name()
	if name == "" {
		panic("runtime: RegisterClass called with empty class name")
	}

	classMu.Lock()
	defer classMu.Unlock()
	if _, exists := classRegistry[name]; exists {
		return
	}
	classRegistry[name] = c
}

// ClearClasses removes all registered classes. It is intended for tests.
func ClearClasses() {
	classMu.Lock()
	defer classMu.Unlock()
	classRegistry = make(map[string]ProviderClass)
}

// MustRegisterClass is like RegisterClass but returns the class so it can
// be chained in var initializations.
func MustRegisterClass(c ProviderClass) ProviderClass {
	RegisterClass(c)
	return c
}

// GetClass returns a registered class by name.
func GetClass(name string) (ProviderClass, bool) {
	classMu.RLock()
	defer classMu.RUnlock()
	c, ok := classRegistry[name]
	return c, ok
}

// ClassNames returns the names of all registered classes in sorted order.
func ClassNames() []string {
	classMu.RLock()
	defer classMu.RUnlock()
	names := make([]string, 0, len(classRegistry))
	for name := range classRegistry {
		names = append(names, name)
	}
	return names
}
