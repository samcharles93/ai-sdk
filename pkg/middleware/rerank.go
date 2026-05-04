package middleware

import "github.com/samcharles93/ai-sdk/pkg/rerank"

// RerankMiddleware wraps a rerank.Provider to intercept and potentially
// modify calls. Middleware can be stacked to compose behaviour.
type RerankMiddleware func(rerank.Provider) rerank.Provider

// ChainRerank composes multiple RerankMiddleware into a single middleware.
// It uses the generic Chain function from chain.go.
func ChainRerank(ms ...RerankMiddleware) RerankMiddleware {
	return ChainGeneric[rerank.Provider, RerankMiddleware](ms...)
}
