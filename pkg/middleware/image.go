package middleware

import "github.com/samcharles93/ai-sdk/pkg/image"

// ImageMiddleware wraps an image.Provider to intercept and potentially
// modify calls. Middleware can be stacked to compose behaviour.
type ImageMiddleware func(image.Provider) image.Provider

// ChainImage composes multiple ImageMiddleware into a single middleware.
// It uses the generic Chain function from chain.go.
func ChainImage(ms ...ImageMiddleware) ImageMiddleware {
	return ChainGeneric[image.Provider, ImageMiddleware](ms...)
}
