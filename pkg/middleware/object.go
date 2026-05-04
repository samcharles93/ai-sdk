package middleware

import "github.com/samcharles93/ai-sdk/pkg/object"

// ObjectMiddleware wraps an object.Provider to intercept and potentially
// modify calls. Middleware can be stacked to compose behaviour.
type ObjectMiddleware func(object.Provider) object.Provider

// ChainObject composes multiple ObjectMiddleware into a single middleware.
// It uses the generic Chain function from chain.go.
func ChainObject(ms ...ObjectMiddleware) ObjectMiddleware {
	return ChainGeneric[object.Provider, ObjectMiddleware](ms...)
}
