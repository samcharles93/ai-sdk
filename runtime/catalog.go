package runtime

import "github.com/samcharles93/ai-sdk/catalog"

// The models.dev catalog lives in package catalog; these names keep the
// runtime's API.
type (
	CatalogProvider = catalog.Provider
	CatalogModel    = catalog.Model
	ReasoningOption = catalog.ReasoningOption
	CatalogOptions  = catalog.Options
	Catalog         = catalog.Catalog
)

// DefaultCatalogURL is the public models.dev provider API endpoint.
const DefaultCatalogURL = catalog.DefaultURL

// ErrCatalogUnavailable is returned when the catalog cannot be loaded from any
// source.
var ErrCatalogUnavailable = catalog.ErrUnavailable

// NPMClassMapping maps models.dev npm package identifiers to the provider class
// names registered by RegisterBuiltinClasses.
var NPMClassMapping = catalog.NPMClassMapping

// NewCatalog returns an empty catalog configured by opts.
func NewCatalog(opts CatalogOptions) *Catalog { return catalog.New(opts) }
