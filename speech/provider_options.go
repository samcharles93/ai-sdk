package speech

import (
	"encoding/json"
	"fmt"
)

// Options is the common OpenAI-compatible speech option set. Providers read
// it from a request's ProviderOptions bucket under their own name (for
// example "openai" or "groq") via [ProviderOptionsFor].
//
// Values set on the request itself always take precedence over the same
// option here.
type Options struct {
	// Instructions is optional voice/style guidance, where supported.
	Instructions string `json:"instructions,omitempty"`
	// Speed is the speaking rate multiplier, used when the request does not
	// set one.
	Speed float64 `json:"speed,omitempty"`
	// SampleRate is the desired output sample rate in Hz, where supported.
	SampleRate int `json:"sample_rate,omitempty"`
	// Voice overrides the provider default voice, where supported.
	Voice string `json:"voice,omitempty"`
	// Format overrides the provider default output format, where supported.
	Format string `json:"format,omitempty"`
}

// ProviderOptionsFor extracts the provider-specific options bucket from a
// ProviderOptions map (typically [GenerateSpeechRequest.ProviderOptions])
// into a typed value.
//
// providerName is the key used to namespace the bucket — by convention the
// provider's [Provider.Name] return value, e.g. "openai", "groq".
//
// Two input shapes are supported transparently:
//
//   - The bucket is already the typed Options struct (or a pointer to one) —
//     it is returned as-is.
//   - The bucket is a map[string]any (e.g. constructed from JSON) — it is
//     re-marshalled and decoded into T using encoding/json so that JSON tags
//     on T's fields are honoured.
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
		return zero, fmt.Errorf("speech.ProviderOptionsFor[%T]: marshal: %w", zero, err)
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return zero, fmt.Errorf("speech.ProviderOptionsFor[%T]: unmarshal: %w", zero, err)
	}
	return out, nil
}
