package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/samcharles93/ai-sdk/pkg/chat"
	"github.com/samcharles93/ai-sdk/pkg/core"
)

// ErrProviderNotFound is returned when a model reference cannot be mapped
// to a configured or catalog provider.
var ErrProviderNotFound = fmt.Errorf("runtime: provider not found")

// ErrClassNotFound is returned when a provider's class is not registered.
var ErrClassNotFound = fmt.Errorf("runtime: provider class not found")

// ErrCapabilityNotSupported is returned when the resolved provider cannot
// satisfy the requested capability.
var ErrCapabilityNotSupported = fmt.Errorf("runtime: capability not supported")

// Runtime resolves model references to provider instances and exposes a
// high-level chat/embed API. It caches provider instances so repeated
// calls to the same provider/model/URL reuse the same underlying HTTP
// client.
type Runtime struct {
	catalog   *Catalog
	config    Config
	instances map[string]ProviderSet
	mu        sync.RWMutex
}

// NewRuntime creates a Runtime from the supplied configuration. It does
// not load the catalog automatically; call LoadCatalog (or supply a
// pre-populated Catalog) before making calls.
func NewRuntime(cfg Config) *Runtime {
	return &Runtime{
		catalog:   NewCatalog(CatalogOptions{}),
		config:    cfg,
		instances: make(map[string]ProviderSet),
	}
}

// NewRuntimeWithCatalog creates a Runtime that uses an already-populated
// catalog.
func NewRuntimeWithCatalog(cfg Config, catalog *Catalog) *Runtime {
	r := NewRuntime(cfg)
	r.catalog = catalog
	return r
}

// LoadCatalog fetches or loads the models.dev catalog according to the
// runtime configuration. If cfg.CatalogURL or cfg.CatalogCachePath are
// set, they override the catalog defaults.
func (r *Runtime) LoadCatalog(ctx context.Context) error {
	opts := CatalogOptions{
		URL:       r.config.CatalogURL,
		CachePath: r.config.CatalogCachePath,
	}
	if r.catalog == nil {
		r.catalog = NewCatalog(opts)
	} else {
		r.catalog.opts = opts
	}
	return r.catalog.Load(ctx)
}

// ResolveAuth resolves credentials for a configured provider using its
// registered auth resolver.
func (r *Runtime) ResolveAuth(ctx context.Context, providerID string) (AuthResult, error) {
	cfg, err := r.buildProviderConfig(providerID)
	if err != nil {
		return AuthResult{}, err
	}
	return resolveAuth(ctx, cfg)
}

// Catalog returns the runtime's catalog. It may be nil until LoadCatalog
// is called.
func (r *Runtime) Catalog() *Catalog {
	return r.catalog
}

// ModelRef is a parsed model reference of the form "provider/model" or
// just "model" when a default provider is configured.
type ModelRef struct {
	ProviderID string
	ModelID    string
}

// ParseModelRef splits a model reference into provider and model IDs. If
// the reference has no "/" separator and a default provider is configured,
// the default provider is used.
func (r *Runtime) ParseModelRef(ref string) (ModelRef, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ModelRef{}, fmt.Errorf("%w: empty model reference", ErrProviderNotFound)
	}

	providerID, modelID, hasSlash := strings.Cut(ref, "/")
	if !hasSlash {
		if r.config.DefaultProvider == "" {
			return ModelRef{}, fmt.Errorf("%w: model reference %q has no provider prefix and no default provider is configured", ErrProviderNotFound, ref)
		}
		return ModelRef{ProviderID: r.config.DefaultProvider, ModelID: ref}, nil
	}

	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	if providerID == "" {
		return ModelRef{}, fmt.Errorf("%w: empty provider in model reference %q", ErrProviderNotFound, ref)
	}
	if modelID == "" {
		return ModelRef{}, fmt.Errorf("%w: empty model in model reference %q", ErrProviderNotFound, ref)
	}
	return ModelRef{ProviderID: providerID, ModelID: modelID}, nil
}

// ChatProvider resolves a model reference to a chat.Provider. It returns
// the provider instance and the resolved model ID that should be passed
// to requests.
func (r *Runtime) ChatProvider(ctx context.Context, ref string) (chat.Provider, string, error) {
	mref, err := r.ParseModelRef(ref)
	if err != nil {
		return nil, "", err
	}
	model, err := r.resolveModel(mref)
	if err != nil {
		return nil, "", err
	}
	set, err := r.providerSetFor(ctx, mref.ProviderID, model)
	if err != nil {
		return nil, "", err
	}
	if set.Chat == nil {
		return nil, "", fmt.Errorf("%w: provider %q does not support chat", ErrCapabilityNotSupported, mref.ProviderID)
	}
	return set.Chat, model.ID, nil
}

// Chat performs a non-streaming chat completion for the given model
// reference. The model field inside opts is overwritten with the
// resolved model ID.
func (r *Runtime) Chat(ctx context.Context, ref string, opts core.GenerateOptions) (core.GenerateResult, error) {
	provider, modelID, err := r.ChatProvider(ctx, ref)
	if err != nil {
		return core.GenerateResult{}, err
	}
	opts.Model = modelID
	return core.GenerateText(ctx, provider, opts)
}

// ChatStream performs a streaming chat completion for the given model
// reference.
func (r *Runtime) ChatStream(ctx context.Context, ref string, opts core.GenerateOptions) (core.StreamResult, error) {
	provider, modelID, err := r.ChatProvider(ctx, ref)
	if err != nil {
		return core.StreamResult{}, err
	}
	opts.Model = modelID
	return core.StreamText(ctx, provider, opts)
}

// Models returns the resolved model information for a provider, merged
// from configured overrides and the catalog.
func (r *Runtime) Models(providerID string) ([]ModelInfo, error) {
	id := normaliseProviderID(providerID)
	cfg, cfgOK := r.config.ProviderByID(id)

	configured := make(map[string]ModelConfig)
	if cfgOK {
		for _, m := range cfg.Models {
			configured[strings.TrimSpace(m.ID)] = m
		}
	}

	var catalogModels []CatalogModel
	if r.catalog != nil {
		var err error
		catalogModels, err = r.catalog.Models(id)
		if err != nil && !cfgOK {
			return nil, fmt.Errorf("%w: %q", ErrProviderNotFound, providerID)
		}
	}

	seen := make(map[string]struct{})
	out := make([]ModelInfo, 0, len(configured)+len(catalogModels))

	for _, cm := range catalogModels {
		mid := strings.TrimSpace(cm.ID)
		if mid == "" {
			continue
		}
		seen[mid] = struct{}{}
		info := catalogToModelInfo(id, cm)
		if override, ok := configured[mid]; ok {
			info = mergeModelInfoWithConfig(info, override)
		}
		out = append(out, info)
	}

	for mid, mc := range configured {
		if _, ok := seen[mid]; ok {
			continue
		}
		out = append(out, configToModelInfo(id, mc))
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// resolveModel merges catalog and configured metadata for a model reference.
func (r *Runtime) resolveModel(ref ModelRef) (ModelInfo, error) {
	models, err := r.Models(ref.ProviderID)
	if err != nil {
		return ModelInfo{}, err
	}
	for _, m := range models {
		if m.ID == ref.ModelID {
			return m, nil
		}
	}

	// If the model is not advertised anywhere, fall back to a synthetic
	// ModelInfo using the provider base URL. This lets users target
	// arbitrary model IDs on custom providers.
	cfg, ok := r.config.ProviderByID(ref.ProviderID)
	if !ok {
		if r.catalog == nil {
			return ModelInfo{}, fmt.Errorf("%w: provider %q", ErrProviderNotFound, ref.ProviderID)
		}
		if _, ok := r.catalog.Provider(ref.ProviderID); !ok {
			return ModelInfo{}, fmt.Errorf("%w: provider %q", ErrProviderNotFound, ref.ProviderID)
		}
	}
	return ModelInfo{
		ID:         ref.ModelID,
		ProviderID: ref.ProviderID,
		URL:        strings.TrimRight(cfg.BaseURL, "/"),
	}, nil
}

// providerSetFor returns a cached or freshly-built provider set for the
// given provider/model. The cache key includes the model URL so that
// per-model endpoints (e.g. MaaS) get distinct HTTP clients.
func (r *Runtime) providerSetFor(ctx context.Context, providerID string, model ModelInfo) (ProviderSet, error) {
	url := model.providerURL("")
	key := fmt.Sprintf("%s\x00%s\x00%s", normaliseProviderID(providerID), model.ID, url)

	r.mu.RLock()
	set, ok := r.instances[key]
	r.mu.RUnlock()
	if ok {
		return set, nil
	}

	cfg, err := r.buildProviderConfig(providerID)
	if err != nil {
		return ProviderSet{}, err
	}

	class, err := r.classForProvider(cfg)
	if err != nil {
		return ProviderSet{}, err
	}

	set, err = class.New(ctx, cfg, model)
	if err != nil {
		return ProviderSet{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.instances[key]; ok {
		return existing, nil
	}
	r.instances[key] = set
	return set, nil
}

// buildProviderConfig constructs a ProviderConfig for a provider ID using
// the runtime configuration first, then falling back to the catalog.
func (r *Runtime) buildProviderConfig(providerID string) (ProviderConfig, error) {
	id := normaliseProviderID(providerID)
	if cfg, ok := r.config.ProviderByID(id); ok {
		return r.enrichFromCatalog(cfg), nil
	}

	if r.catalog == nil {
		return ProviderConfig{}, fmt.Errorf("%w: %q", ErrProviderNotFound, providerID)
	}

	_, ok := r.catalog.Provider(id)
	if !ok {
		return ProviderConfig{}, fmt.Errorf("%w: %q", ErrProviderNotFound, providerID)
	}

	baseURL, _ := r.catalog.API(id)
	npm, _ := r.catalog.NPM(id)
	class := "openai-compatible"
	if npm != "" {
		if mapped, ok := NPMClassMapping[npm]; ok {
			class = mapped
		}
	}

	return ProviderConfig{
		ID:      id,
		Class:   class,
		BaseURL: baseURL,
		// Auth is left empty; callers using catalog-only providers should
		// set it via the runtime config.
	}, nil
}

// enrichFromCatalog fills fields a configured provider left blank using the
// models.dev catalog, so callers may register a provider with just an ID and
// auth and inherit the rest. An empty BaseURL falls back to the provider's
// published api endpoint; an empty Class falls back to the npm-package mapping.
// Explicitly set values are never overridden.
func (r *Runtime) enrichFromCatalog(cfg ProviderConfig) ProviderConfig {
	if r.catalog == nil {
		return cfg
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		if api, ok := r.catalog.API(cfg.ID); ok {
			cfg.BaseURL = api
		}
	}
	if strings.TrimSpace(cfg.Class) == "" {
		if npm, ok := r.catalog.NPM(cfg.ID); ok {
			if mapped, ok := NPMClassMapping[npm]; ok {
				cfg.Class = mapped
			}
		}
	}
	return cfg
}

// classForProvider returns the ProviderClass for cfg, using an explicit
// Class if set, otherwise the catalog npm mapping, otherwise the generic
// openai-compatible class.
func (r *Runtime) classForProvider(cfg ProviderConfig) (ProviderClass, error) {
	className := strings.TrimSpace(cfg.Class)
	if className == "" {
		if r.catalog != nil {
			if npm, ok := r.catalog.NPM(cfg.ID); ok {
				if mapped, ok := NPMClassMapping[npm]; ok {
					className = mapped
				}
			}
		}
	}
	if className == "" {
		className = "openai-compatible"
	}
	class, ok := GetClass(className)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrClassNotFound, className)
	}
	return class, nil
}

func catalogToModelInfo(providerID string, cm CatalogModel) ModelInfo {
	return ModelInfo{
		ID:               cm.ID,
		ProviderID:       providerID,
		Name:             cm.Name,
		Reasoning:        cm.Reasoning,
		ReasoningOptions: cm.ReasoningOptions,
		ToolCall:         cm.ToolCall,
		StructuredOutput: cm.Structured,
		Temperature:      cm.Temperature,
		ContextWindow:    cm.Limit.Context,
		MaxOutputTokens:  cm.Limit.Output,
		Cost: CostConfig{
			Input:      cm.Cost.Input,
			Output:     cm.Cost.Output,
			CacheRead:  cm.Cost.CacheRead,
			CacheWrite: cm.Cost.CacheWrite,
		},
	}
}

func configToModelInfo(providerID string, mc ModelConfig) ModelInfo {
	return ModelInfo{
		ID:               mc.ID,
		ProviderID:       providerID,
		Name:             mc.Name,
		URL:              strings.TrimRight(mc.URL, "/"),
		Reasoning:        mc.Reasoning,
		ToolCall:         mc.ToolCall,
		StructuredOutput: mc.StructuredOutput,
		Temperature:      mc.Temperature,
		ContextWindow:    mc.ContextWindow,
		MaxOutputTokens:  mc.MaxOutputTokens,
		Cost:             mc.Cost,
		Extra:            mc.Extra,
	}
}

func mergeModelInfoWithConfig(base ModelInfo, mc ModelConfig) ModelInfo {
	if strings.TrimSpace(mc.Name) != "" {
		base.Name = mc.Name
	}
	if strings.TrimSpace(mc.URL) != "" {
		base.URL = strings.TrimRight(mc.URL, "/")
	}
	if mc.ContextWindow > 0 {
		base.ContextWindow = mc.ContextWindow
	}
	if mc.MaxOutputTokens > 0 {
		base.MaxOutputTokens = mc.MaxOutputTokens
	}
	if mc.Reasoning {
		base.Reasoning = true
	}
	if mc.ToolCall {
		base.ToolCall = true
	}
	if mc.StructuredOutput {
		base.StructuredOutput = true
	}
	if mc.Temperature {
		base.Temperature = true
	}
	if mc.Cost.Input != 0 {
		base.Cost.Input = mc.Cost.Input
	}
	if mc.Cost.Output != 0 {
		base.Cost.Output = mc.Cost.Output
	}
	if mc.Cost.CacheRead != 0 {
		base.Cost.CacheRead = mc.Cost.CacheRead
	}
	if mc.Cost.CacheWrite != 0 {
		base.Cost.CacheWrite = mc.Cost.CacheWrite
	}
	return base
}
