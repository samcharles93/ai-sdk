// Package image defines provider-agnostic image generation types and
// the ImageProvider interface that all image model backends implement.
//
// The types in this package form the canonical request/response shape
// used across the SDK. Concrete providers translate to and from these
// types so that higher-level code can remain backend-independent.
package image
