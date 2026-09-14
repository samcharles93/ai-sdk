package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

// TestOpenAICompatibleSpeechDefaults covers the voice and format precedence
// chain for the generic class: request > per-model Extra > provider Options.
// With no default at all, Voice is required rather than silently inheriting
// OpenAI's "alloy".
func TestOpenAICompatibleSpeechDefaults(t *testing.T) {
	tests := []struct {
		name       string
		options    map[string]any
		extra      map[string]any
		voice      string
		wantVoice  string
		wantFormat string
		wantErr    bool
	}{
		{name: "no default requires voice", wantErr: true},
		{
			name:      "provider option voice",
			options:   map[string]any{"speech_default_voice": "af_heart"},
			wantVoice: "af_heart",
		},
		{
			name:       "model extra wins over provider option",
			options:    map[string]any{"speech_default_voice": "af_heart", "speech_default_format": "mp3"},
			extra:      map[string]any{"speech_default_voice": "am_michael"},
			wantVoice:  "am_michael",
			wantFormat: "mp3",
		},
		{
			name:      "request voice wins over both",
			options:   map[string]any{"speech_default_voice": "af_heart"},
			extra:     map[string]any{"speech_default_voice": "am_michael"},
			voice:     "af_bella",
			wantVoice: "af_bella",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var requests int
			var gotBody map[string]any
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				_, _ = w.Write([]byte("audio"))
			}))
			defer ts.Close()

			RegisterBuiltinClasses()
			rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
				"local-tts": {
					ID:      "local-tts",
					Class:   "openai-compatible",
					BaseURL: ts.URL,
					Auth:    AuthConfig{Type: AuthTypeNone},
					Options: tc.options,
					Models:  []ModelConfig{{ID: "kokoro", Extra: tc.extra}},
				},
			}})

			_, err := rt.Speech(context.Background(), "local-tts/kokoro", speech.GenerateSpeechRequest{
				Text:  "hello",
				Voice: tc.voice,
			})
			if tc.wantErr {
				if !errors.Is(err, speech.ErrInvalidRequest) {
					t.Fatalf("expected ErrInvalidRequest, got %v", err)
				}
				if requests != 0 {
					t.Fatalf("requests = %d, want 0 when voice is required", requests)
				}
				return
			}
			if err != nil {
				t.Fatalf("Speech: %v", err)
			}
			if gotBody["voice"] != tc.wantVoice {
				t.Errorf("voice = %v, want %s", gotBody["voice"], tc.wantVoice)
			}
			if tc.wantFormat != "" && gotBody["response_format"] != tc.wantFormat {
				t.Errorf("response_format = %v, want %s", gotBody["response_format"], tc.wantFormat)
			}
		})
	}
}

// TestOpenAIClassKeepsAlloyDefault verifies the named openai class still
// applies its documented defaults; they are configured explicitly rather than
// inherited by every OpenAI-compatible backend.
func TestOpenAIClassKeepsAlloyDefault(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte("audio"))
	}))
	defer ts.Close()

	RegisterBuiltinClasses()
	rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
		"openai": {
			ID:      "openai",
			Class:   "openai",
			BaseURL: ts.URL,
			Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
		},
	}})

	if _, err := rt.Speech(context.Background(), "openai/gpt-4o-mini-tts", speech.GenerateSpeechRequest{Text: "hello"}); err != nil {
		t.Fatalf("Speech: %v", err)
	}
	if gotBody["voice"] != "alloy" || gotBody["response_format"] != "mp3" {
		t.Fatalf("voice/format = %v/%v, want alloy/mp3", gotBody["voice"], gotBody["response_format"])
	}
}
