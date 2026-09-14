package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

// TestGroqClassSurfacesSpeech verifies the groq class advertises
// CapabilitySpeech and the granular builder surfaces a speech.Provider on the
// ProviderSet, guarding the Groq TTS wiring.
func TestGroqClassSurfacesSpeech(t *testing.T) {
	RegisterBuiltinClasses()

	cls, ok := GetClass("groq")
	if !ok {
		t.Fatal("groq class not registered")
	}
	if !cls.Supports(CapabilitySpeech) {
		t.Fatal("groq class must advertise CapabilitySpeech")
	}

	cfg := ProviderConfig{
		ID:      "groq",
		Class:   "groq",
		BaseURL: "https://api.groq.com/openai/v1",
		Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
	}
	model := ModelInfo{ID: "playai-tts", ProviderID: "groq", URL: cfg.BaseURL}

	set, err := cls.New(context.Background(), cfg, model)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if set.Speech == nil {
		t.Fatal("expected groq ProviderSet to surface a speech.Provider")
	}
	if !set.Has(CapabilitySpeech) {
		t.Fatal("set must report speech support")
	}
}

// TestOpenAICompatibleClassSurfacesSpeech verifies the generic class
// advertises CapabilitySpeech and resolves a self-hosted OpenAI-compatible
// TTS server through Runtime.Speech, with no new provider code. This is the
// Kokoro-FastAPI / speaches / openedai-speech path.
func TestOpenAICompatibleClassSurfacesSpeech(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Errorf("path = %q, want /v1/audio/speech", r.URL.Path)
		}
		_, _ = w.Write([]byte("local-audio"))
	}))
	defer ts.Close()

	RegisterBuiltinClasses()

	cls, ok := GetClass("openai-compatible")
	if !ok {
		t.Fatal("openai-compatible class not registered")
	}
	if !cls.Supports(CapabilitySpeech) {
		t.Fatal("openai-compatible class must advertise CapabilitySpeech")
	}

	rt := NewRuntime(Config{
		Providers: map[string]ProviderConfig{
			"local-tts": {
				ID:      "local-tts",
				Class:   "openai-compatible",
				BaseURL: ts.URL,
				Auth:    AuthConfig{Type: AuthTypeNone},
			},
		},
	})

	resp, err := rt.Speech(context.Background(), "local-tts/kokoro", speech.GenerateSpeechRequest{
		Text:  "hello from Go",
		Voice: "af_heart",
	})
	if err != nil {
		t.Fatalf("Speech: %v", err)
	}
	if string(resp.Audio) != "local-audio" {
		t.Fatalf("audio = %q, want local-audio", resp.Audio)
	}
	if resp.Format != "mp3" {
		t.Fatalf("format = %q, want mp3 default", resp.Format)
	}
}

// TestOpenAICompatibleSpeechRequiresExplicitNoneAuth guards the producer-side
// auth requirement: an empty Auth.Type defaults to api_key, so a self-hosted
// keyless server must be configured with AuthTypeNone explicitly.
func TestOpenAICompatibleSpeechRequiresExplicitNoneAuth(t *testing.T) {
	RegisterBuiltinClasses()

	rt := NewRuntime(Config{
		Providers: map[string]ProviderConfig{
			"local-tts": {
				ID:      "local-tts",
				Class:   "openai-compatible",
				BaseURL: "http://127.0.0.1:1",
			},
		},
	})

	_, err := rt.Speech(context.Background(), "local-tts/kokoro", speech.GenerateSpeechRequest{Text: "hi"})
	if err == nil {
		t.Fatal("expected an auth resolution error")
	}
	if !strings.Contains(err.Error(), "no api_key") {
		t.Fatalf("error = %v, want a missing api_key error", err)
	}
}
