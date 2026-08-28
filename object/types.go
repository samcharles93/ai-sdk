package object

import "encoding/json"

// Object is a simple named artefact produced by providers.
// Name is a short identifier (for example a filename) and Content holds
// the textual payload.
type Object struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ObjectResult is the abstract result returned by providers. It is kept as
// an alias for any so provider implementations may return either a concrete
// Object or any richer structure without forcing a single shape across
// providers.
type ObjectResult any

// Request is a provider-agnostic object generation request. Only Model is
// required by convention; providers should treat zero values as "unspecified"
// and apply their own defaults.
type Request struct {
	Model     string `json:"model"`
	Prompt    string `json:"prompt,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	// Schema is a JSON Schema constraining the shape of the generated
	// object. Providers that support structured output should use it to
	// guide generation. Callers using core.GenerateTypedObject /
	// core.StreamTypedObject don't need to set this themselves — it's
	// derived automatically from the requested Go type.
	Schema          json.RawMessage `json:"schema,omitempty"`
	ProviderOptions map[string]any  `json:"provider_options,omitempty"`
}

// Warning is a non-fatal provider message attached to responses.
type Warning struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
}

// Response is a non-streaming object generation result.
type Response struct {
	ID       string    `json:"id,omitempty"`
	Model    string    `json:"model,omitempty"`
	Object   Object    `json:"object"`
	Warnings []Warning `json:"warnings,omitempty"`
}
