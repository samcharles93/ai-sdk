package embed

import "context"

// Provider is implemented by embedding model backends. Implementations
// translate between the provider-agnostic Request/Response types defined in
// this package and their underlying API.
type Provider interface {
	// Name returns a short, stable identifier for the provider
	// (for example, "openai", "ollama").
	Name() string

	// Embed produces one embedding vector per entry in req.Inputs, in the
	// same order.
	Embed(ctx context.Context, req Request) (Response, error)
}
