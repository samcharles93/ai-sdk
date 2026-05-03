// Package video defines provider-agnostic video generation types and
// the VideoProvider interface that all video model backends implement.
//
// The types in this package form the canonical request/response shape
// used across the SDK. Concrete providers translate to and from these
// types so that higher-level code can remain backend-independent.
package video
