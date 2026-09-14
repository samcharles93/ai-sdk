//go:build live

package openai

import (
	"context"
	"os"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

// TestOpenAIStreamSpeechLive calls the real OpenAI speech endpoint over both
// streaming wire modes: SSE for models that support it and raw chunked audio
// for the tts-1 family. It is gated behind the `live` build tag and skipped
// unless OPENAI_API_KEY is set, so it never runs under `task check` or CI.
func TestOpenAIStreamSpeechLive(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set; skipping live streaming speech test")
	}
	p, err := New(Config{APIKey: key, Streaming: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := []struct {
		name  string
		model string
	}{
		{name: "sse", model: "gpt-4o-mini-tts"},
		{name: "raw", model: "tts-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stream, err := p.StreamSpeech(context.Background(), speech.GenerateSpeechRequest{
				Model: tc.model,
				Text:  "The quick brown fox jumps over the lazy dog.",
				Voice: "alloy",
			})
			if err != nil {
				t.Fatalf("StreamSpeech: %v", err)
			}
			defer stream.Close()

			var total, chunks int
			var format string
			var usage *speech.Usage
			for {
				chunk, err := stream.Next(context.Background())
				if err != nil {
					t.Fatalf("Next: %v", err)
				}
				if chunk.Done {
					format = chunk.Format
					usage = chunk.Usage
					break
				}
				if len(chunk.Data) > 0 {
					total += len(chunk.Data)
					chunks++
					if format == "" {
						format = chunk.Format
					}
				}
			}
			if total == 0 {
				t.Fatal("no audio received")
			}
			t.Logf("%s streamed %d bytes in %d chunks (format %s, usage %+v)", tc.model, total, chunks, format, usage)
		})
	}
}
