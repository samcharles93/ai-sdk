// Package openaiobject provides an OpenAI-backed implementation of the
// object.Provider interface for schema-constrained object generation.
//
// It translates between the provider-agnostic object.Request/Response
// types and OpenAI's Chat Completions API. When a request carries a JSON
// Schema, the provider uses response_format.json_schema with strict mode;
// otherwise it falls back to response_format.json_object. Both
// non-streaming (GenerateObject) and streaming (StreamObject) modes are
// supported.
package openaiobject
