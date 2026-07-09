// Package transcribe defines provider-agnostic audio transcription types
// and the TranscriptionProvider interface that all speech-to-text
// backends implement.
//
// The types in this package form the canonical request/response shape
// used across the SDK. Concrete providers translate to and from these
// types so that higher-level code can remain backend-independent.
package transcribe
