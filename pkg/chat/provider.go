package chat

import "context"

// Provider is implemented by chat model backends. Implementations translate
// between the provider-agnostic Request/Response/Chunk types defined in this
// package and their underlying API.
type Provider interface {
	// Name returns a short, stable identifier for the provider
	// (for example, "openai", "anthropic", "ollama").
	Name() string

	// Chat performs a non-streaming chat completion.
	Chat(ctx context.Context, req Request) (Response, error)

	// ChatStream performs a streaming chat completion. Callers must Close
	// the returned Stream when finished.
	ChatStream(ctx context.Context, req Request) (Stream, error)
}

// Stream is an iterator over Chunks produced by a streaming chat completion.
//
// Next returns io.EOF (and a zero Chunk) when the stream is exhausted; any
// other non-nil error indicates a stream failure. Callers must Close the
// stream exactly once, even after receiving io.EOF, to release resources.
type Stream interface {
	// Next returns the next chunk. It returns io.EOF when the stream is
	// exhausted.
	Next(ctx context.Context) (Chunk, error)

	// Close releases resources associated with the stream.
	Close() error
}
