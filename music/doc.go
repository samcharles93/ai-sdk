// Package music defines provider-agnostic music generation types and the
// Provider interface that all music-generation backends implement.
//
// The types in this package form the canonical request/response shape used
// across the SDK. Concrete providers translate to and from these types so
// that higher-level code can remain backend-independent.
package music
