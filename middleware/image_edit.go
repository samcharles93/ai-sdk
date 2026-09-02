package middleware

import "github.com/samcharles93/ai-sdk/image"

// ImageEditMiddleware wraps an [image.Editor] to intercept and potentially
// modify image-edit calls. Middleware can be stacked to compose behaviour.
type ImageEditMiddleware func(image.Editor) image.Editor

// ChainImageEdit composes multiple ImageEditMiddleware into a single
// middleware. It uses the generic Chain function from chain.go.
func ChainImageEdit(ms ...ImageEditMiddleware) ImageEditMiddleware {
	return ChainGeneric(ms...)
}
