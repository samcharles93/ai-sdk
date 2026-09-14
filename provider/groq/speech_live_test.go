//go:build live

package groq

import (
	"context"
	"os"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

// TestGroqSpeechSynthesisLive calls the real Groq speech endpoint. It is
// gated behind a `live` build tag and skipped unless GROQ_API_KEY is set, so
// it never runs under `task check` or CI.
func TestGroqSpeechSynthesisLive(t *testing.T) {
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		t.Skip("GROQ_API_KEY not set; skipping live speech test")
	}
	p, err := New(Config{APIKey: key})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model:  "playai-tts",
		Voice:  "Fritz-PlayAI",
		Text:   "The quick brown fox jumps over the lazy dog.",
		Format: "wav",
	})
	if err != nil {
		t.Fatalf("GenerateSpeech: %v", err)
	}
	if len(resp.Audio) == 0 {
		t.Fatal("GenerateSpeech: empty audio")
	}
	if resp.Format != "wav" {
		t.Fatalf("GenerateSpeech: format = %q, want wav", resp.Format)
	}
	t.Logf("groq synthesised %d bytes of %s audio", len(resp.Audio), resp.Format)
}
