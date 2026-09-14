package runtime

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

func TestSpeechStreamOpenAIClass(t *testing.T) {
	want := []byte("streamed-audio")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"speech.audio.delta\",\"audio\":%q}\n\n", base64.StdEncoding.EncodeToString(want))
		_, _ = io.WriteString(w, "data: {\"type\":\"speech.audio.done\"}\n\n")
	}))
	defer srv.Close()

	RegisterBuiltinClasses()
	rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
		"openai": {
			ID:      "openai",
			Class:   "openai",
			BaseURL: srv.URL,
			Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
		},
	}})

	stream, err := rt.SpeechStream(context.Background(), "openai/gpt-4o-mini-tts", speech.GenerateSpeechRequest{Text: "hi"})
	if err != nil {
		t.Fatalf("SpeechStream: %v", err)
	}
	defer stream.Close()

	var got []byte
	for {
		chunk, err := stream.Next(context.Background())
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if chunk.Done {
			break
		}
		got = append(got, chunk.Data...)
	}
	if string(got) != string(want) {
		t.Fatalf("audio = %q, want %q", got, want)
	}
}

func TestSpeechStreamUnsupported(t *testing.T) {
	RegisterBuiltinClasses()

	t.Run("provider without streamer", func(t *testing.T) {
		rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
			"groq": {
				ID:      "groq",
				Class:   "groq",
				BaseURL: "http://127.0.0.1:1",
				Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
			},
		}})
		_, err := rt.SpeechStream(context.Background(), "groq/canopylabs/orpheus-v1-english", speech.GenerateSpeechRequest{Text: "hi"})
		if !errors.Is(err, speech.ErrStreamNotSupported) {
			t.Fatalf("error = %v, want ErrStreamNotSupported", err)
		}

		_, _, err = rt.SpeechStreamProvider(context.Background(), "groq/canopylabs/orpheus-v1-english")
		if !errors.Is(err, ErrCapabilityNotSupported) {
			t.Fatalf("SpeechStreamProvider error = %v, want ErrCapabilityNotSupported", err)
		}
	})

	t.Run("generic class streaming disabled", func(t *testing.T) {
		rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
			"local-tts": {
				ID:      "local-tts",
				Class:   "openai-compatible",
				BaseURL: "http://127.0.0.1:1",
				Auth:    AuthConfig{Type: AuthTypeNone},
			},
		}})
		_, err := rt.SpeechStream(context.Background(), "local-tts/kokoro", speech.GenerateSpeechRequest{Text: "hi", Voice: "af_heart"})
		if !errors.Is(err, speech.ErrStreamNotSupported) {
			t.Fatalf("error = %v, want ErrStreamNotSupported", err)
		}
	})
}
