package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultCatalogURL is the public models.dev provider API endpoint.
const DefaultCatalogURL = "https://models.dev/api.json"

// ErrCatalogUnavailable is returned when the catalog cannot be loaded
// from any source.
var ErrCatalogUnavailable = errors.New("runtime: model catalog unavailable")

// CatalogProvider mirrors models.dev's provider entry.
type CatalogProvider struct {
	ID     string                  `json:"id"`
	NPM    string                  `json:"npm,omitzero"`
	API    string                  `json:"api,omitzero"`
	Env    []string                `json:"env,omitzero"`
	Models map[string]CatalogModel `json:"models,omitzero"`
}

// CatalogModel mirrors the per-provider model entries from models.dev.
// The runtime uses these as metadata; it does not enforce that every
// provider exposes every advertised model.
type CatalogModel struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitzero"`
	Family      string `json:"family,omitzero"`
	Attachment  bool   `json:"attachment,omitzero"`
	Reasoning   bool   `json:"reasoning,omitzero"`
	ToolCall    bool   `json:"tool_call,omitzero"`
	Structured  bool   `json:"structured_output,omitzero"`
	Temperature bool   `json:"temperature,omitzero"`
	Modalities  struct {
		Input  []string `json:"input,omitzero"`
		Output []string `json:"output,omitzero"`
	} `json:"modalities"`
	Limit struct {
		Context int `json:"context,omitzero"`
		Output  int `json:"output,omitzero"`
	} `json:"limit"`
	Cost struct {
		Input      float64 `json:"input,omitzero"`
		Output     float64 `json:"output,omitzero"`
		CacheRead  float64 `json:"cache_read,omitzero"`
		CacheWrite float64 `json:"cache_write,omitzero"`
	} `json:"cost"`
}

// ContextWindow returns the model's context limit if known.
func (m CatalogModel) ContextWindow() int {
	return m.Limit.Context
}

// MaxOutputTokens returns the model's output limit if known.
func (m CatalogModel) MaxOutputTokens() int {
	return m.Limit.Output
}

// CatalogOptions configures where and how catalog data is loaded.
type CatalogOptions struct {
	// URL is the endpoint to fetch. Defaults to DefaultCatalogURL.
	URL string

	// CachePath is a file path used to store the fetched JSON. Empty
	// disables disk caching.
	CachePath string

	// TTL is the maximum age of a cached file before a network refresh.
	// Zero or negative disables freshness checks (always fetch).
	TTL time.Duration

	// HTTPClient is used for network requests. Nil means a default
	// client with a 30s timeout.
	HTTPClient *http.Client
}

func (o *CatalogOptions) url() string {
	if strings.TrimSpace(o.URL) != "" {
		return strings.TrimRight(o.URL, "/")
	}
	return DefaultCatalogURL
}

func (o *CatalogOptions) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Catalog is the in-memory view of the models.dev provider/model metadata.
type Catalog struct {
	opts      CatalogOptions
	mu        sync.RWMutex
	providers map[string]CatalogProvider
	fetchedAt time.Time
}

// NewCatalog creates an empty catalog. Call Load or Fetch before use.
func NewCatalog(opts CatalogOptions) *Catalog {
	return &Catalog{opts: opts}
}

// Provider returns a provider by id, normalised to lower case.
func (c *Catalog) Provider(id string) (CatalogProvider, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.providers[normalizeProviderID(id)]
	return p, ok
}

// Models returns the models advertised for a provider in deterministic order.
func (c *Catalog) Models(providerID string) ([]CatalogModel, error) {
	p, ok := c.Provider(providerID)
	if !ok {
		return nil, fmt.Errorf("%w: provider %q not found", ErrCatalogUnavailable, providerID)
	}
	ids := make([]string, 0, len(p.Models))
	for id := range p.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]CatalogModel, 0, len(ids))
	for _, id := range ids {
		m := p.Models[id]
		if strings.TrimSpace(m.ID) == "" {
			m.ID = id
		}
		out = append(out, m)
	}
	return out, nil
}

// Model looks up a specific model advertised under a provider.
func (c *Catalog) Model(providerID, modelID string) (CatalogModel, bool) {
	p, ok := c.Provider(providerID)
	if !ok {
		return CatalogModel{}, false
	}
	m, ok := p.Models[strings.TrimSpace(modelID)]
	if !ok {
		return CatalogModel{}, false
	}
	if strings.TrimSpace(m.ID) == "" {
		m.ID = modelID
	}
	return m, true
}

// APIKeyEnv returns the first declared environment variable name for a
// provider's API key, if any.
func (c *Catalog) APIKeyEnv(providerID string) (string, bool) {
	p, ok := c.Provider(providerID)
	if !ok {
		return "", false
	}
	for _, env := range p.Env {
		if strings.TrimSpace(env) != "" {
			return env, true
		}
	}
	return "", false
}

// API returns the provider's default API base URL from the catalog.
func (c *Catalog) API(providerID string) (string, bool) {
	p, ok := c.Provider(providerID)
	if !ok {
		return "", false
	}
	if strings.TrimSpace(p.API) == "" {
		return "", false
	}
	return strings.TrimRight(p.API, "/"), true
}

// NPM returns the provider's npm package identifier from the catalog.
func (c *Catalog) NPM(providerID string) (string, bool) {
	p, ok := c.Provider(providerID)
	if !ok {
		return "", false
	}
	if strings.TrimSpace(p.NPM) == "" {
		return "", false
	}
	return p.NPM, true
}

// Load populates the catalog from the freshest available source: network
// if allowed, otherwise a disk cache, otherwise an embedded snapshot or
// caller-supplied override.
func (c *Catalog) Load(ctx context.Context) error {
	providers, fresh, err := c.readCache()
	if err == nil && fresh {
		c.setProviders(providers, time.Now())
		return nil
	}

	if err := c.Fetch(ctx); err != nil {
		if providers != nil {
			c.setProviders(providers, time.Now().Add(-c.opts.TTL-1))
			return nil
		}
		return err
	}
	return nil
}

// Fetch forces a network refresh and writes the result to disk cache.
func (c *Catalog) Fetch(ctx context.Context) error {
	body, err := c.fetchCatalog(ctx)
	if err != nil {
		return err
	}
	providers, err := parseCatalogProviders(body)
	if err != nil {
		return err
	}
	if c.opts.CachePath != "" {
		_ = c.writeCache(body)
	}
	c.setProviders(providers, time.Now())
	return nil
}

// LoadFromJSON populates the catalog directly from JSON bytes, ignoring
// network and cache. This is useful for embedded snapshots or tests.
func (c *Catalog) LoadFromJSON(data []byte) error {
	providers, err := parseCatalogProviders(data)
	if err != nil {
		return err
	}
	c.setProviders(providers, time.Now())
	return nil
}

// MergeProviders overlays additional providers onto the current catalog.
// Existing entries are merged field-by-field; new entries are added.
func (c *Catalog) MergeProviders(providers map[string]CatalogProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.providers == nil {
		c.providers = make(map[string]CatalogProvider)
	}
	for id, p := range providers {
		key := normalizeProviderID(id)
		existing, ok := c.providers[key]
		if !ok {
			c.providers[key] = p
			continue
		}
		c.providers[key] = mergeCatalogProvider(existing, p)
	}
}

func (c *Catalog) setProviders(providers map[string]CatalogProvider, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers = providers
	c.fetchedAt = at
}

// FetchedAt returns the time the current snapshot was obtained.
func (c *Catalog) FetchedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fetchedAt
}

func (c *Catalog) readCache() (map[string]CatalogProvider, bool, error) {
	if c.opts.CachePath == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(c.opts.CachePath)
	if err != nil {
		return nil, false, err
	}
	providers, err := parseCatalogProviders(data)
	if err != nil {
		return nil, false, err
	}
	info, statErr := os.Stat(c.opts.CachePath)
	fresh := false
	if statErr == nil {
		fresh = c.opts.TTL > 0 && time.Since(info.ModTime()) <= c.opts.TTL
	}
	// Return nil on purpose: stale cache is better than nothing.
	_ = statErr
	return providers, fresh, nil
}

func (c *Catalog) writeCache(body []byte) error {
	dir := filepath.Dir(c.opts.CachePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating catalogue cache dir: %w", err)
	}
	tmp := c.opts.CachePath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("writing catalogue cache: %w", err)
	}
	if err := os.Rename(tmp, c.opts.CachePath); err != nil {
		return fmt.Errorf("finalising catalogue cache: %w", err)
	}
	return nil
}

func (c *Catalog) fetchCatalog(ctx context.Context) ([]byte, error) {
	url := c.opts.url()
	if url == "" {
		return nil, fmt.Errorf("%w: no catalogue URL configured", ErrCatalogUnavailable)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.opts.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch catalog: %v", ErrCatalogUnavailable, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: reading catalog response: %v", ErrCatalogUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: catalog returned %d: %s", ErrCatalogUnavailable, resp.StatusCode, body)
	}
	return body, nil
}

func parseCatalogProviders(data []byte) (map[string]CatalogProvider, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("%w: invalid catalog JSON: %v", ErrCatalogUnavailable, err)
	}
	providers := make(map[string]CatalogProvider)
	for key, raw := range top {
		if key == "$schema" {
			continue
		}
		// Accept both a flat map of providers and a top-level
		// "providers" object.
		if key == "providers" {
			var nested map[string]CatalogProvider
			if err := json.Unmarshal(raw, &nested); err != nil {
				return nil, fmt.Errorf("%w: invalid providers section: %v", ErrCatalogUnavailable, err)
			}
			for nestedKey, p := range nested {
				providers[normalizeProviderID(nestedKey)] = p
			}
			continue
		}
		var p CatalogProvider
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		if len(p.Models) == 0 && strings.TrimSpace(p.API) == "" && strings.TrimSpace(p.NPM) == "" {
			continue
		}
		if strings.TrimSpace(p.ID) == "" {
			p.ID = key
		}
		providers[normalizeProviderID(key)] = p
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("%w: catalog did not contain any providers", ErrCatalogUnavailable)
	}
	return providers, nil
}

func normalizeProviderID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

func mergeCatalogProvider(base, override CatalogProvider) CatalogProvider {
	result := base
	if strings.TrimSpace(override.ID) != "" {
		result.ID = override.ID
	}
	if strings.TrimSpace(override.NPM) != "" {
		result.NPM = override.NPM
	}
	if strings.TrimSpace(override.API) != "" {
		result.API = override.API
	}
	if len(override.Env) > 0 {
		result.Env = override.Env
	}
	if result.Models == nil {
		result.Models = make(map[string]CatalogModel)
	}
	for key, model := range override.Models {
		existing, exists := result.Models[key]
		if !exists {
			result.Models[key] = model
			continue
		}
		result.Models[key] = mergeCatalogModel(existing, model)
	}
	return result
}

func mergeCatalogModel(base, override CatalogModel) CatalogModel {
	result := base
	if strings.TrimSpace(override.ID) != "" {
		result.ID = override.ID
	}
	if strings.TrimSpace(override.Name) != "" {
		result.Name = override.Name
	}
	if override.Limit.Context > 0 {
		result.Limit.Context = override.Limit.Context
	}
	if override.Limit.Output > 0 {
		result.Limit.Output = override.Limit.Output
	}
	if override.Reasoning {
		result.Reasoning = true
	}
	if override.Attachment {
		result.Attachment = true
	}
	if override.ToolCall {
		result.ToolCall = true
	}
	if override.Structured {
		result.Structured = true
	}
	if override.Temperature {
		result.Temperature = true
	}
	if override.Cost.Input != 0 {
		result.Cost.Input = override.Cost.Input
	}
	if override.Cost.Output != 0 {
		result.Cost.Output = override.Cost.Output
	}
	if override.Cost.CacheRead != 0 {
		result.Cost.CacheRead = override.Cost.CacheRead
	}
	if override.Cost.CacheWrite != 0 {
		result.Cost.CacheWrite = override.Cost.CacheWrite
	}
	return result
}
