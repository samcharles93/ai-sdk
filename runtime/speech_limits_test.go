package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

// TestSpeechInputLimits covers the per-class defaults for the injectable
// input limit and the per-model/provider overrides, asserting that an
// oversized input is rejected before any HTTP request is made.
func TestSpeechInputLimits(t *testing.T) {
	RegisterBuiltinClasses()

	t.Run("openai class defaults to 4096", func(t *testing.T) {
		var requests int
		ts := testAudioServer(t, &requests)
		rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
			"openai": {
				ID:      "openai",
				Class:   "openai",
				BaseURL: ts.URL,
				Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
			},
		}})

		_, err := rt.Speech(context.Background(), "openai/tts-1", speech.GenerateSpeechRequest{
			Text:  strings.Repeat("a", 4097),
			Voice: "alloy",
		})
		if !errors.Is(err, speech.ErrInvalidRequest) {
			t.Fatalf("expected ErrInvalidRequest for 4097 characters, got %v", err)
		}
		if requests != 0 {
			t.Fatalf("requests = %d, want 0 for an oversized input", requests)
		}

		if _, err := rt.Speech(context.Background(), "openai/tts-1", speech.GenerateSpeechRequest{
			Text:  strings.Repeat("a", 4096),
			Voice: "alloy",
		}); err != nil {
			t.Fatalf("Speech at the limit: %v", err)
		}
		if requests != 1 {
			t.Fatalf("requests = %d, want 1 at the limit", requests)
		}
	})

	t.Run("groq class defaults to 200", func(t *testing.T) {
		var requests int
		ts := testAudioServer(t, &requests)
		rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
			"groq": {
				ID:      "groq",
				Class:   "groq",
				BaseURL: ts.URL,
				Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
			},
		}})

		_, err := rt.Speech(context.Background(), "groq/canopylabs/orpheus-v1-english", speech.GenerateSpeechRequest{
			Text:  strings.Repeat("a", 201),
			Voice: "hannah",
		})
		if !errors.Is(err, speech.ErrInvalidRequest) {
			t.Fatalf("expected ErrInvalidRequest for 201 characters, got %v", err)
		}
		if requests != 0 {
			t.Fatalf("requests = %d, want 0 for an oversized input", requests)
		}
	})

	t.Run("generic class has no client-side cap", func(t *testing.T) {
		var requests int
		ts := testAudioServer(t, &requests)
		rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
			"local-tts": {
				ID:      "local-tts",
				Class:   "openai-compatible",
				BaseURL: ts.URL,
				Auth:    AuthConfig{Type: AuthTypeNone},
			},
		}})

		if _, err := rt.Speech(context.Background(), "local-tts/kokoro", speech.GenerateSpeechRequest{
			Text:  strings.Repeat("a", 5000),
			Voice: "af_heart",
		}); err != nil {
			t.Fatalf("Speech: %v", err)
		}
		if requests != 1 {
			t.Fatalf("requests = %d, want 1 (no client-side cap)", requests)
		}
	})

	t.Run("per-model extra overrides the class default", func(t *testing.T) {
		var requests int
		ts := testAudioServer(t, &requests)
		rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
			"local-tts": {
				ID:      "local-tts",
				Class:   "openai-compatible",
				BaseURL: ts.URL,
				Auth:    AuthConfig{Type: AuthTypeNone},
				Models: []ModelConfig{
					{ID: "limited", Extra: map[string]any{"max_input_chars": 10}},
				},
			},
		}})

		_, err := rt.Speech(context.Background(), "local-tts/limited", speech.GenerateSpeechRequest{
			Text:  strings.Repeat("a", 11),
			Voice: "af_heart",
		})
		if !errors.Is(err, speech.ErrInvalidRequest) {
			t.Fatalf("expected ErrInvalidRequest for 11 characters, got %v", err)
		}
		if requests != 0 {
			t.Fatalf("requests = %d, want 0 for an oversized input", requests)
		}

		if _, err := rt.Speech(context.Background(), "local-tts/limited", speech.GenerateSpeechRequest{
			Text:  strings.Repeat("a", 10),
			Voice: "af_heart",
		}); err != nil {
			t.Fatalf("Speech at the limit: %v", err)
		}
		if requests != 1 {
			t.Fatalf("requests = %d, want 1 at the limit", requests)
		}
	})

	t.Run("extra beats provider options", func(t *testing.T) {
		var requests int
		ts := testAudioServer(t, &requests)
		rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
			"local-tts": {
				ID:      "local-tts",
				Class:   "openai-compatible",
				BaseURL: ts.URL,
				Auth:    AuthConfig{Type: AuthTypeNone},
				Options: map[string]any{"max_input_chars": 100},
				Models: []ModelConfig{
					{ID: "limited", Extra: map[string]any{"max_input_chars": 10}},
				},
			},
		}})

		_, err := rt.Speech(context.Background(), "local-tts/limited", speech.GenerateSpeechRequest{
			Text:  strings.Repeat("a", 11),
			Voice: "af_heart",
		})
		if !errors.Is(err, speech.ErrInvalidRequest) {
			t.Fatalf("expected the per-model limit (10) to win, got %v", err)
		}
		if requests != 0 {
			t.Fatalf("requests = %d, want 0 for an oversized input", requests)
		}
	})
}
