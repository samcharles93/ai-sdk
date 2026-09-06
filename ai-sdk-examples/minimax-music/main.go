// Command minimax-music demonstrates the MiniMax music generation domain
// (music.Provider), wired through the ai-sdk runtime like any other domain.
//
// If MINIMAX_API_KEY is set, it generates a short instrumental track from a
// prompt. Otherwise it prints the API pattern for reference.
//
//	Usage:
//	  MINIMAX_API_KEY=... go run ./ai-sdk-examples/minimax-music/
//	  (without key — prints API documentation)
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/samcharles93/ai-sdk/music"
	"github.com/samcharles93/ai-sdk/runtime"
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

	runtime.RegisterBuiltinClasses()

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

	resp, err := rt.Music(context.Background(), "minimax/music-3.0", music.GenerateMusicRequest{
		Prompt:       "Cinematic ambient, warm and uplifting, perfect for a sunrise montage",
		Instrumental: true,
		Format:       "mp3",
	})
	if err != nil {
		return fmt.Errorf("generate music: %w", err)
	}

	fmt.Printf("Generated %d bytes of %s audio\n", len(resp.Audio), resp.Format)
	fmt.Println("Music generation is synchronous on MiniMax; the audio bytes are returned directly.")
	return nil
}

func printDocs() {
	fmt.Println("MiniMax Music Generation — music.Provider:")
	fmt.Println("  MINIMAX_API_KEY=... go run ./ai-sdk-examples/minimax-music/")
	fmt.Println()
	fmt.Println("API pattern:")
	fmt.Println("  rt := runtime.NewRuntime(runtime.Config{Providers: map[string]runtime.ProviderConfig{")
	fmt.Println("      \"minimax\": {ID: \"minimax\", Class: \"minimax\", BaseURL: \"https://api.minimax.io\",")
	fmt.Println("        Auth: runtime.AuthConfig{Type: runtime.AuthTypeAPIKey, APIKey: apiKey}},")
	fmt.Println("  }})")
	fmt.Println("  resp, err := rt.Music(ctx, \"minimax/music-3.0\", music.GenerateMusicRequest{")
	fmt.Println("      Prompt: \"Cinematic ambient, warm and uplifting\",")
	fmt.Println("      Instrumental: true, Format: \"mp3\",")
	fmt.Println("  })")
	fmt.Println()
	fmt.Println("Music generation is synchronous on MiniMax (returns audio bytes directly).")
}
