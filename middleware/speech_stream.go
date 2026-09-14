package middleware

import "github.com/samcharles93/ai-sdk/speech"

// SpeechStreamMiddleware wraps a speech.Streamer to intercept and potentially
// modify calls. It is the streaming counterpart to SpeechMiddleware, matching
// the optional-capability pattern used for image editing.
type SpeechStreamMiddleware func(speech.Streamer) speech.Streamer

// ChainSpeechStream composes multiple SpeechStreamMiddleware into a single
// middleware. It uses the generic Chain function from chain.go.
func ChainSpeechStream(ms ...SpeechStreamMiddleware) SpeechStreamMiddleware {
	return ChainGeneric(ms...)
}
