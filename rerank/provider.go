package rerank

import "context"

// Provider is implemented by reranking model backends. Implementations
// translate between the provider-agnostic types defined in this package
// and their underlying API.
type Provider interface {
	// Name returns a short, stable identifier for the provider
	// (for example, "togetherai", "cohere").
	Name() string

	// Rerank re-orders documents by relevance to the query.
	Rerank(ctx context.Context, req Request) (Response, error)
}
