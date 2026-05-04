// Command video-generation demonstrates video generation from text prompts
// using the AI SDK with the xAI provider. If XAI_API_KEY is set, it creates a
// short video. Otherwise it prints the API pattern for reference.
//
//	Usage:
//	  XAI_API_KEY=... go run ./ai-sdk-examples/video-generation/
//	  (without key — prints API documentation)
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/samcharles93/ai-sdk/pkg/core"
	"github.com/samcharles93/ai-sdk/pkg/provider/xai"
	"github.com/samcharles93/ai-sdk/pkg/video"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	apiKey := os.Getenv("XAI_API_KEY")
	if apiKey == "" {
		printDocs()
		return nil
	}

	provider, err := xai.New(xai.Config{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("create xai provider: %w", err)
	}

	ctx := context.Background()
	resp, err := core.GenerateVideo(ctx, provider, video.GenerateVideoRequest{
		Model:  "grok-video",
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
	fmt.Println("Video Generation API — usage:")
	fmt.Println("  XAI_API_KEY=... go run ./ai-sdk-examples/video-generation/")
	fmt.Println()
	fmt.Println("Providers implementing video.Provider:")
	fmt.Println("  - pkg/provider/xai/ — xAI grok-video")
	fmt.Println()
	fmt.Println("API pattern:")
	fmt.Println("  resp, err := core.GenerateVideo(ctx, provider, video.GenerateVideoRequest{")
	fmt.Println("      Model:  \"grok-video\",")
	fmt.Println("      Prompt: \"A drone flyover...\",")
	fmt.Println("  })")
	fmt.Println()
	fmt.Println("Note: video generation is expensive and may take minutes to complete.")
	fmt.Println("The provider handles polling internally.")
}
