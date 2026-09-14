package runtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

// TestOpenAICompatibleVoiceDiscovery verifies the generic class surfaces a
// speech.VoiceLister that reads the self-hosted server's /audio/voices
// endpoint, both through a direct type assertion and the domain Client.
func TestOpenAICompatibleVoiceDiscovery(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/voices" {
			t.Errorf("path = %q, want /v1/audio/voices", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"voices":[{"id":"af_heart","name":"af_heart","language":"en-us","gender":"female"},"am_michael"]}`))
	}))
	defer ts.Close()

	RegisterBuiltinClasses()
	rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
		"local-tts": {
			ID:      "local-tts",
			Class:   "openai-compatible",
			BaseURL: ts.URL,
			Auth:    AuthConfig{Type: AuthTypeNone},
		},
	}})

	provider, modelID, err := rt.SpeechProvider(context.Background(), "local-tts/kokoro")
	if err != nil {
		t.Fatalf("SpeechProvider: %v", err)
	}
	lister, ok := provider.(speech.VoiceLister)
	if !ok {
		t.Fatal("expected the openai-compatible speech provider to implement speech.VoiceLister")
	}

	voices, err := lister.ListVoices(context.Background(), modelID)
	if err != nil {
		t.Fatalf("ListVoices: %v", err)
	}
	if len(voices) != 2 {
		t.Fatalf("len(voices) = %d, want 2", len(voices))
	}
	if voices[0].ID != "af_heart" || voices[0].Language != "en-us" || voices[0].Model != "kokoro" {
		t.Fatalf("voices[0] = %+v, want the discovered af_heart scoped to kokoro", voices[0])
	}
	if voices[1].ID != "am_michael" {
		t.Fatalf("voices[1] = %+v, want am_michael", voices[1])
	}

	client := speech.NewClient(provider)
	if _, err := client.ListVoices(context.Background(), modelID); err != nil {
		t.Fatalf("Client.ListVoices: %v", err)
	}
}

func TestGroqVoiceListingUnsupported(t *testing.T) {
	RegisterBuiltinClasses()
	rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
		"groq": {
			ID:      "groq",
			Class:   "groq",
			BaseURL: "http://127.0.0.1:1",
			Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
		},
	}})

	provider, modelID, err := rt.SpeechProvider(context.Background(), "groq/canopylabs/orpheus-v1-english")
	if err != nil {
		t.Fatalf("SpeechProvider: %v", err)
	}
	if _, err := speech.NewClient(provider).ListVoices(context.Background(), modelID); !errors.Is(err, speech.ErrVoiceListingNotSupported) {
		t.Fatalf("error = %v, want ErrVoiceListingNotSupported", err)
	}
}
