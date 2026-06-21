// Package runtime provides a provider-agnostic AI runtime for the ai-sdk.
//
// It glues together the domain interfaces (chat, embed, etc.), provider
// implementations, and the models.dev catalog so that applications can
// resolve a model reference such as "openai/gpt-4o" into a working chat
// provider without hardcoding every provider themselves.
//
// The runtime is intentionally layered above pkg/core: it imports domain
// interfaces and provider implementations, then delegates the actual
// chat/embed orchestration to core.GenerateText / core.StreamText.
//
// Key abstractions:
//
//   - ProviderClass: a pluggable factory that turns a ProviderConfig into
//     a ProviderSet of domain implementations. Built-in classes include
//     "openai-compatible", which uses pkg/provider/openai with arbitrary
//     base URLs.
//   - Catalog: the in-memory view of the models.dev provider/model
//     metadata, loaded from a network snapshot, an embedded fallback, or
//     caller-supplied JSON.
//   - Runtime: the public entry point. It resolves model references to
//     provider instances, caches them, and exposes Chat/ChatStream calls.
//
// Applications can add custom provider classes by calling RegisterClass
// before constructing a Runtime, or by populating ProviderConfig entries
// with a known class name.
package runtime
