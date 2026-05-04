package middleware

import "github.com/samcharles93/ai-sdk/pkg/embed"

// EmbedMiddleware wraps an embed.Provider to intercept and potentially
// modify calls. Middleware can be stacked to compose behaviour.
type EmbedMiddleware func(embed.Provider) embed.Provider

// ChainEmbed composes multiple EmbedMiddleware into a single middleware.
// It uses the generic Chain function from chain.go.
func ChainEmbed(ms ...EmbedMiddleware) EmbedMiddleware {
	return ChainGeneric[embed.Provider, EmbedMiddleware](ms...)
}
