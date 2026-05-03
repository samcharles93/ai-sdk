package embed

import (
	"encoding/json"
	"fmt"
)

// ProviderOptionsFor extracts the provider-specific options bucket from a
// ProviderOptions map (typically [Request.ProviderOptions]) into a typed
// value.
//
// See the chat package's equivalent helper for the full description; the
// behaviour is identical.
func ProviderOptionsFor[T any](po map[string]any, providerName string) (T, error) {
	var zero T
	if po == nil || providerName == "" {
		return zero, nil
	}
	raw, ok := po[providerName]
	if !ok || raw == nil {
		return zero, nil
	}
	if v, ok := raw.(T); ok {
		return v, nil
	}
	if v, ok := raw.(*T); ok && v != nil {
		return *v, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return zero, fmt.Errorf("embed.ProviderOptionsFor[%T]: marshal: %w", zero, err)
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return zero, fmt.Errorf("embed.ProviderOptionsFor[%T]: unmarshal: %w", zero, err)
	}
	return out, nil
}
