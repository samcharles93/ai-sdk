// Package rerank defines provider-agnostic document reranking types and
// the Provider interface that all reranking model backends implement.
//
// Reranking takes a query and a set of documents, and re-orders the
// documents by their relevance to the query. This is commonly used in
// RAG pipelines to improve retrieval quality.
//
// The types in this package form the canonical request/response shape
// used across the SDK. Concrete providers translate to and from these
// types so that higher-level code can remain backend-independent.
package rerank
