package middleware

import "github.com/samcharles93/ai-sdk/pkg/speech"

// SpeechMiddleware wraps a speech.Provider to intercept and potentially
// modify calls. Middleware can be stacked to compose behaviour.
type SpeechMiddleware func(speech.Provider) speech.Provider

// ChainSpeech composes multiple SpeechMiddleware into a single middleware.
// It uses the generic Chain function from chain.go.
func ChainSpeech(ms ...SpeechMiddleware) SpeechMiddleware {
	return ChainGeneric(ms...)
}
