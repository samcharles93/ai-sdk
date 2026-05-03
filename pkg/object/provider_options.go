package object

import (
	"encoding/json"
	"fmt"
)

// ProviderOptionsFor extracts a provider-specific options bucket from a
// ProviderOptions map (typically [Request.ProviderOptions]) into a typed
// value. Behaviour mirrors the helpers used in other domain packages: if the
// bucket is already of type T it is returned as-is; if it is a plain map it
// will be JSON round-tripped into T so struct tags are honoured.
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
		return zero, fmt.Errorf("object.ProviderOptionsFor[%T]: marshal: %w", zero, err)
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return zero, fmt.Errorf("object.ProviderOptionsFor[%T]: unmarshal: %w", zero, err)
	}
	return out, nil
}
