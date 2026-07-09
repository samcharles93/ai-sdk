package video

import (
	"encoding/json"
	"fmt"
)

// ProviderOptionsFor extracts the provider-specific options bucket from a
// ProviderOptions map (typically [GenerateVideoRequest.ProviderOptions])
// into a typed value.
//
// providerName is the key used to namespace the bucket — by convention
// the provider's [Provider.Name] return value, e.g. "xai".
//
// Two input shapes are supported transparently:
//
//   - The bucket is already the typed Options struct (or a pointer to one) —
//     it is returned as-is.
//   - The bucket is a map[string]any (e.g. constructed from JSON) — it is
//     re-marshalled and decoded into T using encoding/json so that JSON
//     tags on T's fields are honoured.
//
// If po is nil or the providerName key is absent, the zero value of T is
// returned with a nil error.
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
		return zero, fmt.Errorf("video.ProviderOptionsFor[%T]: marshal: %w", zero, err)
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return zero, fmt.Errorf("video.ProviderOptionsFor[%T]: unmarshal: %w", zero, err)
	}
	return out, nil
}
