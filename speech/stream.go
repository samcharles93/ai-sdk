package speech

import "context"

// SpeechStream is an iterator over speech chunks produced by a streaming
// synthesis. Next returns chunks until the final Done chunk, then io.EOF.
type SpeechStream interface {
	// Next returns the next chunk. It returns io.EOF after the Done chunk.
	Next(ctx context.Context) (SpeechChunk, error)

	// Close releases resources associated with the stream.
	Close() error
}
