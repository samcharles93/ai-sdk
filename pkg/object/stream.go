package object

import "context"

// ObjectChunk is a partial object emitted during streaming.
type ObjectChunk struct {
	// Delta is the JSON patch or partial object text.
	Delta string `json:"delta"`

	// Done is true when the stream is complete.
	Done bool `json:"done"`
}

// ObjectStream is an iterator over object chunks.
type ObjectStream interface {
	// Next returns the next chunk. It returns io.EOF when the stream is
	// exhausted.
	Next(ctx context.Context) (ObjectChunk, error)

	// Close releases resources associated with the stream.
	Close() error
}
