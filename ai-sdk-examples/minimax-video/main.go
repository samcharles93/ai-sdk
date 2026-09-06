// Command minimax-video demonstrates the reference MiniMax provider, wired
// through the ai-sdk runtime the way an operator (or archie) would for a
// provider that is not in the models.dev catalog.
//
// MiniMax is registered as the built-in "minimax" provider class and, besides
// this video example, serves chat (OpenAI-compatible), image, and speech. It
// resolves only when the runtime config supplies a provider entry with model
// IDs (e.g. MiniMax-H3, image-01, speech-2.8-hd) and an API key. If
// MINIMAX_API_KEY is set, it submits a short text-to-video job (which may take
// minutes, polling internally). Otherwise it prints the config + resolve
// pattern for reference.
//
//	Usage:
//	  MINIMAX_API_KEY=... go run ./ai-sdk-examples/minimax-video/
//	  (without key — prints API documentation)
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/samcharles93/ai-sdk/runtime"
	"github.com/samcharles93/ai-sdk/video"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	apiKey := os.Getenv("MINIMAX_API_KEY")
	if apiKey == "" {
		printDocs()
		return nil
	}

	// Register the built-in provider classes (includes minimax) once at startup.
	runtime.RegisterBuiltinClasses()

	// A non-catalog provider is wired by config: class "minimax", a model ID,
	// and the API key. This is the custom-class pattern the reference provider
	// demonstrates for archie adoption.
	cfg := runtime.Config{
		Providers: map[string]runtime.ProviderConfig{
			"minimax": {
				ID:      "minimax",
				Class:   "minimax",
				BaseURL: "https://api.minimax.io",
				Auth: runtime.AuthConfig{
					Type:   runtime.AuthTypeAPIKey,
					APIKey: apiKey,
				},
			},
		},
	}
	rt := runtime.NewRuntime(cfg)

	resp, err := rt.Video(context.Background(), "minimax/MiniMax-H3", video.GenerateVideoRequest{
		Prompt: "A drone flyover of a misty mountain valley at sunrise",
	})
	if err != nil {
		return fmt.Errorf("generate video: %w", err)
	}

	for i, v := range resp.Videos {
		fmt.Printf("Video %d: %s (%s)\n", i+1, v.URL, v.MediaType)
	}
	return nil
}

func printDocs() {
	fmt.Println("MiniMax Video Generation — reference provider (runtime config pattern):")
	fmt.Println("  MINIMAX_API_KEY=... go run ./ai-sdk-examples/minimax-video/")
	fmt.Println()
	fmt.Println("MiniMax is a built-in provider class ('minimax', CapabilityVideo) but is not")
	fmt.Println("published in the models.dev catalog, so it resolves only when the runtime")
	fmt.Println("config supplies a provider entry (class 'minimax', a model ID, and its API key).")
	fmt.Println()
	fmt.Println("Config + resolve pattern:")
	fmt.Println("  runtime.RegisterBuiltinClasses()  // includes minimax")
	fmt.Println("  rt := runtime.NewRuntime(runtime.Config{Providers: map[string]runtime.ProviderConfig{")
	fmt.Println("      \"minimax\": {ID: \"minimax\", Class: \"minimax\", BaseURL: \"https://api.minimax.io\",")
	fmt.Println("        Auth: runtime.AuthConfig{Type: runtime.AuthTypeAPIKey, APIKey: apiKey}},")
	fmt.Println("  }})")
	fmt.Println("  resp, err := rt.Video(ctx, \"minimax/MiniMax-H3\", video.GenerateVideoRequest{")
	fmt.Println("      Prompt: \"A drone flyover...\",")
	fmt.Println("  })")
	fmt.Println()
	fmt.Println("Note: video generation is expensive and may take minutes to complete.")
	fmt.Println("The provider handles the submit-and-poll loop internally.")
	fmt.Println()
	fmt.Println("MiniMax also serves chat (OpenAI-compatible), image, and speech through the")
	fmt.Println("same runtime config: rt.Chat/rt.Image/rt.Speech with the appropriate model refs.")
	fmt.Println()
	fmt.Println("For a provider not shipped here, register a custom runtime.ProviderClass")
	fmt.Println("and return a ProviderSet with the domain interface(s) you implement.")
}
