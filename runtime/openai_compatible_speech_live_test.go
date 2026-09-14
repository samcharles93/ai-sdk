//go:build live

package runtime

import (
	"context"
	"os"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

// TestOpenAICompatibleSpeechLive exercises the generic openai-compatible
// speech path end-to-end against a real self-hosted server. It is gated
// behind the `live` build tag and skipped unless AI_SDK_TTS_BASE_URL is set,
// so it never runs under `task check` or CI.
//
// Bring up a server first, for example:
//
//	docker run -p 8880:8880 ghcr.io/remsky/kokoro-fastapi-cpu:latest
//
// then export AI_SDK_TTS_BASE_URL (and optionally AI_SDK_TTS_MODEL,
// AI_SDK_TTS_VOICE, AI_SDK_TTS_FORMAT, AI_SDK_TTS_TEXT) and run:
//
//	task test:live:tts
func TestOpenAICompatibleSpeechLive(t *testing.T) {
	baseURL := os.Getenv("AI_SDK_TTS_BASE_URL")
	if baseURL == "" {
		t.Skip("AI_SDK_TTS_BASE_URL not set; skipping live self-hosted TTS test")
	}
	model := envOr("AI_SDK_TTS_MODEL", "kokoro")
	voice := envOr("AI_SDK_TTS_VOICE", "af_heart")
	format := envOr("AI_SDK_TTS_FORMAT", "mp3")
	text := envOr("AI_SDK_TTS_TEXT", "The quick brown fox jumps over the lazy dog.")

	RegisterBuiltinClasses()
	rt := NewRuntime(Config{Providers: map[string]ProviderConfig{
		"local-tts": {
			ID:      "local-tts",
			Class:   "openai-compatible",
			BaseURL: baseURL,
			Auth:    AuthConfig{Type: AuthTypeNone},
		},
	}})

	resp, err := rt.Speech(context.Background(), "local-tts/"+model, speech.GenerateSpeechRequest{
		Text:   text,
		Voice:  voice,
		Format: format,
	})
	if err != nil {
		t.Fatalf("Speech: %v", err)
	}
	if len(resp.Audio) == 0 {
		t.Fatal("Speech: empty audio")
	}
	if resp.Format != format {
		t.Fatalf("format = %q, want %q", resp.Format, format)
	}
	if !audioMagicOK(format, resp.Audio) {
		head := resp.Audio
		if len(head) > 16 {
			head = head[:16]
		}
		t.Fatalf("audio does not look like %s (first bytes % x)", format, head)
	}
	t.Logf("self-hosted TTS synthesised %d bytes of %s audio (%s/%s)", len(resp.Audio), resp.Format, model, voice)
}

// audioMagicOK reports whether b starts with a plausible container header for
// format.
func audioMagicOK(format string, b []byte) bool {
	switch format {
	case "wav":
		return len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WAVE"
	case "mp3":
		if len(b) >= 3 && string(b[0:3]) == "ID3" {
			return true
		}
		// MPEG frame sync: the first eleven bits are set.
		return len(b) >= 2 && b[0] == 0xFF && b[1]&0xE0 == 0xE0
	case "flac":
		return len(b) >= 4 && string(b[0:4]) == "fLaC"
	case "ogg", "opus":
		return len(b) >= 4 && string(b[0:4]) == "OggS"
	default:
		return len(b) > 0
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
