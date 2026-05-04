package middleware

// ChainGeneric composes middleware functions left-to-right. The first
// middleware becomes the outermost wrapper. ChainGeneric is generic over
// the provider type T and accepts middleware defined as a named function
// type M whose underlying type is func(T) T (for example
// "type EmbedMiddleware func(embed.Provider) embed.Provider").
func ChainGeneric[T any, M ~func(T) T](middlewares ...M) M {
	return M(func(next T) T {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	})
}

// Chain composes middleware functions left-to-right and is a convenience
// wrapper over ChainGeneric with the same semantics. Call sites in the repo
// use Chain[T](...) and rely on type inference for the middleware type M.
func Chain[T any, M ~func(T) T](middlewares ...M) M {
	return ChainGeneric[T, M](middlewares...)
}
