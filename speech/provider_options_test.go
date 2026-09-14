package speech

import "testing"

func TestProviderOptionsFor(t *testing.T) {
	t.Run("nil map returns zero value", func(t *testing.T) {
		got, err := ProviderOptionsFor[Options](nil, "groq")
		if err != nil {
			t.Fatalf("ProviderOptionsFor: %v", err)
		}
		if got != (Options{}) {
			t.Fatalf("got %+v, want zero value", got)
		}
	})

	t.Run("absent key returns zero value", func(t *testing.T) {
		got, err := ProviderOptionsFor[Options](map[string]any{"other": map[string]any{"voice": "x"}}, "groq")
		if err != nil {
			t.Fatalf("ProviderOptionsFor: %v", err)
		}
		if got != (Options{}) {
			t.Fatalf("got %+v, want zero value", got)
		}
	})

	t.Run("typed value passes through", func(t *testing.T) {
		want := Options{Voice: "af_heart", SampleRate: 24000}
		got, err := ProviderOptionsFor[Options](map[string]any{"groq": want}, "groq")
		if err != nil {
			t.Fatalf("ProviderOptionsFor: %v", err)
		}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("map bucket round-trips through JSON", func(t *testing.T) {
		got, err := ProviderOptionsFor[Options](map[string]any{"groq": map[string]any{
			"voice":        "hannah",
			"speed":        1.5,
			"sample_rate":  24000,
			"instructions": "speak slowly",
			"format":       "wav",
		}}, "groq")
		if err != nil {
			t.Fatalf("ProviderOptionsFor: %v", err)
		}
		want := Options{Voice: "hannah", Speed: 1.5, SampleRate: 24000, Instructions: "speak slowly", Format: "wav"}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("undecodable bucket returns an error", func(t *testing.T) {
		_, err := ProviderOptionsFor[Options](map[string]any{"groq": map[string]any{
			"voice": map[string]any{"nested": "object"},
		}}, "groq")
		if err == nil {
			t.Fatal("expected a decode error")
		}
	})
}
