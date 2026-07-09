// Package speech defines provider-agnostic speech synthesis types and
// the SpeechProvider interface that all text-to-speech backends implement.
//
// The types in this package form the canonical request/response shape
// used across the SDK. Concrete providers translate to and from these
// types so that higher-level code can remain backend-independent.
package speech
