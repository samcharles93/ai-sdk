// Package object defines provider-agnostic types and the Provider
// interface for object generation operations.
//
// This package sits at the innermost domain layer and must not depend on
// any other internal pkg/ packages. Providers implement the Provider
// interface to translate between their API and the request/response types
// declared here.
package object
