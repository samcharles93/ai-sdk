package middleware

import "github.com/samcharles93/ai-sdk/transcribe"

// TranscribeMiddleware wraps a transcribe.Provider to intercept and potentially
// modify calls. Middleware can be stacked to compose behaviour.
type TranscribeMiddleware func(transcribe.Provider) transcribe.Provider

// ChainTranscribe composes multiple TranscribeMiddleware into a single middleware.
// It uses the generic Chain function from chain.go.
func ChainTranscribe(ms ...TranscribeMiddleware) TranscribeMiddleware {
	return ChainGeneric(ms...)
}
