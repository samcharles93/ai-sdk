// Command speech-to-text demonstrates the transcription API pattern
// using the AI SDK.
//
// This example shows the API pattern for audio transcription. It currently
// prints a placeholder because no provider in this SDK implements
// transcribe.Provider. Provider authors can implement the interface to
// enable the pattern shown here.
//
//	Usage:
//	  go run ./ai-sdk-examples/speech-to-text/
package main

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/pkg/transcribe"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("error: %v\n", err)
		fmt.Println()
		fmt.Println("Note: No provider currently implements transcribe.Provider in this SDK.")
		fmt.Println("This example demonstrates the API pattern only.")
	}
}

func run() error {
	ctx := context.Background()

	// TranscribeRequest is a provider-agnostic audio transcription request.
	// It supports raw audio data, language hints, and provider-specific
	// options.
	req := transcribe.TranscribeRequest{
		Model:    "whisper-1",
		Language: "en",
		// Audio: audioBytes,  // raw audio data ([]byte)
		// Prompt: "Technical interview",  // optional guiding text
	}

	// When a real transcribe provider is available:
	//
	//   result, err := provider.Transcribe(ctx, req)
	//   if err != nil { ... }
	//   fmt.Println("Transcription:", result.Text)
	//   for _, seg := range result.Segments {
	//       fmt.Printf("[%.1fs-%.1fs] %s\n", seg.Start, seg.End, seg.Text)
	//   }

	_ = ctx
	_ = req

	fmt.Println("Transcription API:")
	fmt.Println("  1. Create a transcribe.Provider implementation")
	fmt.Println("  2. Call provider.Transcribe(ctx, req)")
	fmt.Println("  3. The TranscribeResponse includes:")
	fmt.Println("     - Text: full transcribed text")
	fmt.Println("     - Segments: timed segments with start/end times")
	fmt.Println("     - Language: detected language")
	fmt.Println("     - Duration: audio duration in seconds")
	fmt.Println()
	fmt.Println("The transcribe package (pkg/transcribe/) provides:")
	fmt.Println("  - Provider interface with Transcribe method")
	fmt.Println("  - Request/Response types with segment-level detail")
	fmt.Println("  - A thin Client facade with nil-guard")
	fmt.Println("  - Sentinel errors (ErrNoProvider, ErrInvalidRequest)")

	return nil
}
