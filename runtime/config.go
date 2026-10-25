package runtime

import "net/http"

// Config is the declarative runtime configuration. Applications supply
// this (e.g. from JSON/YAML) and the Runtime turns it into working
// provider instances.
type Config struct {
	// DefaultProvider is the provider ID used when a model reference
	// does not include a provider prefix.
	DefaultProvider string `json:"default_provider,omitempty"`

	// Providers are the configured provider instances. The map key is
	// the local provider ID and must match ProviderConfig.ID.
	Providers map[string]ProviderConfig `json:"providers,omitempty"`

	// CatalogURL overrides the default models.dev endpoint. Empty means
	// use DefaultCatalogURL.
	CatalogURL string `json:"catalog_url,omitempty"`

	// CatalogCachePath enables on-disk caching of the fetched catalog.
	CatalogCachePath string `json:"catalog_cache_path,omitempty"`

	// HTTPTransport, when set, carries every provider request (for example
	// an otelhttp-wrapped transport). A ProviderConfig.Transport takes
	// precedence. Nil uses http.DefaultTransport.
	HTTPTransport http.RoundTripper `json:"-"`
}

// ProviderByID returns the provider config with the given id, or false.
func (c Config) ProviderByID(id string) (ProviderConfig, bool) {
	if c.Providers == nil {
		return ProviderConfig{}, false
	}
	p, ok := c.Providers[id]
	return p, ok
}
