// Package middleware provides middleware types for wrapping AI SDK providers.
// Each domain (chat, embed, image, speech, transcribe, video, rerank, object)
// has its own middleware type and Chain function for composing cross-cutting
// concerns like telemetry, retries, circuit breakers, and custom
// transformations.
//
// This is the Go equivalent of the AI SDK's provider middleware system.
package middleware
