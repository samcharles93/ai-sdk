//go:build live

package openai

import (
	"context"
	"os"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
	"github.com/samcharles93/ai-sdk/transcribe"
)

// TestOpenAITranscribeLive verifies the real OpenAI Whisper endpoint via a
// text-to-speech -> speech-to-text round-trip. Gated behind a `live` build
// tag and skipped unless OPENAI_API_KEY is set.
func TestOpenAITranscribeLive(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set; skipping live transcription test")
	}
	audio := liveSpeechAudio(t)
	p, err := New(Config{APIKey: key})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := p.Transcribe(context.Background(), transcribe.TranscribeRequest{
		Model:    "whisper-1",
		Audio:    audio,
		Language: "en",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if len(resp.Text) == 0 {
		t.Fatal("Transcribe: empty text")
	}
	t.Logf("transcribed: %q", resp.Text)
}

// liveSpeechAudio synthesizes a short phrase via OpenAI TTS and returns the
// mp3 bytes, so the transcription test exercises real speech rather than a tone.
func liveSpeechAudio(t *testing.T) []byte {
	t.Helper()
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set; skipping live round-trip test")
	}
	p, err := New(Config{APIKey: key})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model:  "gpt-4o-mini-tts",
		Voice:  "alloy",
		Text:   "The quick brown fox jumps over the lazy dog.",
		Format: "mp3",
	})
	if err != nil {
		t.Fatalf("GenerateSpeech: %v", err)
	}
	return resp.Audio
}
