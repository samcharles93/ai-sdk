package runtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

// speechCatalogJSON carries the modality metadata the gating tests rely on:
// a Groq-style provider with a TTS model, an ASR model and a chat model, plus
// a generic OpenAI-compatible provider with a chat-only and a speech model.
const speechCatalogJSON = `{
  "groq": {
    "id": "groq",
    "npm": "@ai-sdk/groq",
    "api": "https://api.groq.com/openai/v1",
    "models": {
      "canopylabs/orpheus-v1-english": {
        "id": "canopylabs/orpheus-v1-english",
        "modalities": {"input": ["text"], "output": ["audio"]}
      },
      "whisper-large-v3": {
        "id": "whisper-large-v3",
        "modalities": {"input": ["audio"], "output": ["text"]}
      },
      "llama-3.3-70b-versatile": {
        "id": "llama-3.3-70b-versatile",
        "modalities": {"input": ["text"], "output": ["text"]}
      }
    }
  },
  "local-cloud": {
    "id": "local-cloud",
    "npm": "@ai-sdk/openai-compatible",
    "api": "https://gateway.example/v1",
    "models": {
      "chat-model": {
        "id": "chat-model",
        "modalities": {"input": ["text"], "output": ["text"]}
      },
      "gw-tts": {
        "id": "gw-tts",
        "modalities": {"input": ["text"], "output": ["speech"]}
      }
    }
  }
}`

func testSpeechCatalog(t *testing.T) *Catalog {
	t.Helper()
	c := NewCatalog(CatalogOptions{})
	if err := c.LoadFromJSON([]byte(speechCatalogJSON)); err != nil {
		t.Fatalf("LoadFromJSON: %v", err)
	}
	return c
}

// testAudioServer returns a server that answers every speech request with a
// fixed body and counts the requests it saw.
func testAudioServer(t *testing.T, requests *int) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests++
		_, _ = w.Write([]byte("audio"))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestSpeechGatingRejectsNonSpeechModels(t *testing.T) {
	RegisterBuiltinClasses()
	rt := NewRuntimeWithCatalog(Config{Providers: map[string]ProviderConfig{
		"groq": {
			ID:      "groq",
			Class:   "groq",
			BaseURL: "http://127.0.0.1:1",
			Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
		},
	}}, testSpeechCatalog(t))

	for _, model := range []string{"whisper-large-v3", "llama-3.3-70b-versatile"} {
		_, err := rt.Speech(context.Background(), "groq/"+model, speech.GenerateSpeechRequest{Text: "hi"})
		if !errors.Is(err, ErrCapabilityNotSupported) {
			t.Fatalf("Speech(%s) error = %v, want ErrCapabilityNotSupported", model, err)
		}
		if !strings.Contains(err.Error(), "does not declare speech output") {
			t.Fatalf("Speech(%s) error = %v, want the model-gate message", model, err)
		}
	}
}

func TestSpeechGatingAllowsSpeechModel(t *testing.T) {
	var requests int
	ts := testAudioServer(t, &requests)

	RegisterBuiltinClasses()
	rt := NewRuntimeWithCatalog(Config{Providers: map[string]ProviderConfig{
		"groq": {
			ID:      "groq",
			Class:   "groq",
			BaseURL: ts.URL,
			Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
		},
	}}, testSpeechCatalog(t))

	resp, err := rt.Speech(context.Background(), "groq/canopylabs/orpheus-v1-english", speech.GenerateSpeechRequest{
		Text:  "hi",
		Voice: "hannah",
	})
	if err != nil {
		t.Fatalf("Speech: %v", err)
	}
	if string(resp.Audio) != "audio" {
		t.Fatalf("audio = %q, want audio", resp.Audio)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestSpeechGatingHonoursExplicitCapabilities(t *testing.T) {
	t.Run("opt a text-only model into speech", func(t *testing.T) {
		var requests int
		ts := testAudioServer(t, &requests)

		RegisterBuiltinClasses()
		rt := NewRuntimeWithCatalog(Config{Providers: map[string]ProviderConfig{
			"groq": {
				ID:      "groq",
				Class:   "groq",
				BaseURL: ts.URL,
				Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
				Models: []ModelConfig{
					{ID: "whisper-large-v3", Capabilities: []Capability{CapabilitySpeech}},
				},
			},
		}}, testSpeechCatalog(t))

		if _, err := rt.Speech(context.Background(), "groq/whisper-large-v3", speech.GenerateSpeechRequest{
			Text:  "hi",
			Voice: "hannah",
		}); err != nil {
			t.Fatalf("Speech: %v", err)
		}
		if requests != 1 {
			t.Fatalf("requests = %d, want 1", requests)
		}
	})

	t.Run("opt a speech model out", func(t *testing.T) {
		RegisterBuiltinClasses()
		rt := NewRuntimeWithCatalog(Config{Providers: map[string]ProviderConfig{
			"groq": {
				ID:      "groq",
				Class:   "groq",
				BaseURL: "http://127.0.0.1:1",
				Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
				Models: []ModelConfig{
					{ID: "canopylabs/orpheus-v1-english", Capabilities: []Capability{CapabilityChat}},
				},
			},
		}}, testSpeechCatalog(t))

		_, err := rt.Speech(context.Background(), "groq/canopylabs/orpheus-v1-english", speech.GenerateSpeechRequest{Text: "hi"})
		if !errors.Is(err, ErrCapabilityNotSupported) {
			t.Fatalf("error = %v, want ErrCapabilityNotSupported", err)
		}
	})
}

// TestGenericClassSpeechGatingUsesCatalogMetadata pins the documented
// best-effort semantics for the generic openai-compatible class: published
// metadata that lacks speech output refuses the call, speech metadata (or an
// explicit capability) allows it, and unknown metadata fails open.
func TestGenericClassSpeechGatingUsesCatalogMetadata(t *testing.T) {
	t.Run("known chat-only model is refused", func(t *testing.T) {
		RegisterBuiltinClasses()
		rt := NewRuntimeWithCatalog(Config{Providers: map[string]ProviderConfig{
			"local-cloud": {
				ID:      "local-cloud",
				Class:   "openai-compatible",
				BaseURL: "http://127.0.0.1:1",
				Auth:    AuthConfig{Type: AuthTypeNone},
			},
		}}, testSpeechCatalog(t))

		_, err := rt.Speech(context.Background(), "local-cloud/chat-model", speech.GenerateSpeechRequest{Text: "hi"})
		if !errors.Is(err, ErrCapabilityNotSupported) {
			t.Fatalf("error = %v, want ErrCapabilityNotSupported", err)
		}
	})

	t.Run("known speech model resolves", func(t *testing.T) {
		var requests int
		ts := testAudioServer(t, &requests)

		RegisterBuiltinClasses()
		rt := NewRuntimeWithCatalog(Config{Providers: map[string]ProviderConfig{
			"local-cloud": {
				ID:      "local-cloud",
				Class:   "openai-compatible",
				BaseURL: ts.URL,
				Auth:    AuthConfig{Type: AuthTypeNone},
			},
		}}, testSpeechCatalog(t))

		if _, err := rt.Speech(context.Background(), "local-cloud/gw-tts", speech.GenerateSpeechRequest{
			Text:  "hi",
			Voice: "af_heart",
		}); err != nil {
			t.Fatalf("Speech: %v", err)
		}
		if requests != 1 {
			t.Fatalf("requests = %d, want 1", requests)
		}
	})

	t.Run("unknown metadata fails open", func(t *testing.T) {
		var requests int
		ts := testAudioServer(t, &requests)

		RegisterBuiltinClasses()
		rt := NewRuntimeWithCatalog(Config{Providers: map[string]ProviderConfig{
			"local-cloud": {
				ID:      "local-cloud",
				Class:   "openai-compatible",
				BaseURL: ts.URL,
				Auth:    AuthConfig{Type: AuthTypeNone},
			},
		}}, testSpeechCatalog(t))

		if _, err := rt.Speech(context.Background(), "local-cloud/undocumented-model", speech.GenerateSpeechRequest{
			Text:  "hi",
			Voice: "af_heart",
		}); err != nil {
			t.Fatalf("Speech: %v", err)
		}
		if requests != 1 {
			t.Fatalf("requests = %d, want 1", requests)
		}
	})
}

func TestChatResolutionIgnoresModalities(t *testing.T) {
	var requests int
	ts := testAudioServer(t, &requests)

	RegisterBuiltinClasses()
	rt := NewRuntimeWithCatalog(Config{Providers: map[string]ProviderConfig{
		"groq": {
			ID:      "groq",
			Class:   "groq",
			BaseURL: ts.URL,
			Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
		},
	}}, testSpeechCatalog(t))

	provider, modelID, err := rt.ChatProvider(context.Background(), "groq/canopylabs/orpheus-v1-english")
	if err != nil {
		t.Fatalf("ChatProvider: %v", err)
	}
	if provider == nil || modelID != "canopylabs/orpheus-v1-english" {
		t.Fatalf("provider/modelID = %v/%q, want a provider and the resolved model", provider, modelID)
	}
}

func TestMergeCatalogModelKeepsModalities(t *testing.T) {
	var base CatalogModel
	base.ID = "m"
	base.Modalities.Input = []string{"text"}
	base.Modalities.Output = []string{"audio"}

	var override CatalogModel
	override.ID = "m"

	merged := mergeCatalogModel(base, override)
	if len(merged.Modalities.Output) != 1 || merged.Modalities.Output[0] != "audio" {
		t.Fatalf("merged output modalities = %v, want [audio]", merged.Modalities.Output)
	}

	var replacement CatalogModel
	replacement.ID = "m"
	replacement.Modalities.Output = []string{"text"}

	replaced := mergeCatalogModel(base, replacement)
	if len(replaced.Modalities.Output) != 1 || replaced.Modalities.Output[0] != "text" {
		t.Fatalf("replaced output modalities = %v, want [text]", replaced.Modalities.Output)
	}
}
