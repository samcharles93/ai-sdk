// Package chat defines provider-agnostic chat types and the Provider
// interface that all model backends implement.
//
// The types in this package form the canonical request/response shape used
// across the SDK. Concrete providers (OpenAI, Anthropic, local models, ...)
// translate to and from these types so that higher-level code can remain
// backend-independent.
package chat
