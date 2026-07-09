package object

import "context"

// Provider is implemented by object generation backends. Implementations
// translate between the provider-agnostic Request/Response types defined in
// this package and their underlying API.
type Provider interface {
	// Name returns a short, stable identifier for the provider
	// (for example, "openai", "ollama").
	Name() string

	// GenerateObject performs a non-streaming object generation operation.
	GenerateObject(ctx context.Context, req Request) (ObjectResult, error)

	// StreamObject performs a streaming object generation. Callers must
	// Close the returned ObjectStream when finished.
	StreamObject(ctx context.Context, req Request) (ObjectStream, error)
}
