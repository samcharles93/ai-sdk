package chat

import (
	"encoding/json"
	"fmt"
)

// ProviderOptionsFor extracts the provider-specific options bucket from a
// ProviderOptions map (typically [Request.ProviderOptions] or
// [Message.ProviderOptions]) into a typed value.
//
// providerName is the key used to namespace the bucket — by convention
// the provider's [Provider.Name] return value, e.g. "openai", "ollama".
//
// Two input shapes are supported transparently:
//
//   - The bucket is already the typed Options struct (or a pointer to one)
//     — it is returned as-is.
//   - The bucket is a map[string]any (e.g. constructed from JSON) — it is
//     re-marshalled and decoded into T using encoding/json so that JSON
//     tags on T's fields are honoured.
//
// If po is nil or the providerName key is absent, the zero value of T is
// returned with a nil error. Decoding errors are wrapped.
//
// Example provider-side use:
//
//	type Options struct {
//	    ReasoningEffort string `json:"reasoning_effort,omitempty"`
//	}
//	opts, err := chat.ProviderOptionsFor[Options](req.ProviderOptions, "openai")
func ProviderOptionsFor[T any](po map[string]any, providerName string) (T, error) {
	var zero T
	if po == nil || providerName == "" {
		return zero, nil
	}
	raw, ok := po[providerName]
	if !ok || raw == nil {
		return zero, nil
	}

	// Already the right concrete type — common when callers construct
	// the request with a typed struct rather than a generic map.
	if v, ok := raw.(T); ok {
		return v, nil
	}
	// Pointer to the right concrete type.
	if v, ok := raw.(*T); ok && v != nil {
		return *v, nil
	}

	// Otherwise round-trip via JSON.
	b, err := json.Marshal(raw)
	if err != nil {
		return zero, fmt.Errorf("chat.ProviderOptionsFor[%T]: marshal: %w", zero, err)
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return zero, fmt.Errorf("chat.ProviderOptionsFor[%T]: unmarshal: %w", zero, err)
	}
	return out, nil
}
