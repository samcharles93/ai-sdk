// Package embed defines provider-agnostic embedding types and the Provider
// interface that all embedding model backends implement.
//
// It is the parallel of chat for vector embeddings: the types in this
// package form the canonical request/response shape used across the SDK,
// and concrete providers translate to and from them. Providers in
// provider/* may implement both chat.Provider and embed.Provider when
// the underlying backend exposes both capabilities.
package embed
