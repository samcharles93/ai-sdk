//go:build live

package openai

import (
	"context"
	"os"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

// TestOpenAISpeechSynthesisLive calls the real OpenAI TTS endpoint. It is
// gated behind a `live` build tag and skipped unless OPENAI_API_KEY is set,
// so it never runs under `task check` or CI.
func TestOpenAISpeechSynthesisLive(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set; skipping live speech test")
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
	if len(resp.Audio) == 0 {
		t.Fatal("GenerateSpeech: empty audio")
	}
	if resp.Format == "" {
		t.Fatal("GenerateSpeech: empty format")
	}
}
