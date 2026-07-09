// Command image-generation demonstrates image generation from text prompts
// using the AI SDK with the TogetherAI provider. If TOGETHER_API_KEY is set,
// it calls the API and writes the output as a PNG file. Otherwise it prints
// the API pattern for reference.
//
//	Usage:
//	  TOGETHER_API_KEY=... go run ./ai-sdk-examples/image-generation/
//	  (without key — prints API documentation)
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/samcharles93/ai-sdk/core"
	"github.com/samcharles93/ai-sdk/image"
	"github.com/samcharles93/ai-sdk/provider/togetherai"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	apiKey := os.Getenv("TOGETHER_API_KEY")
	if apiKey == "" {
		printDocs()
		return nil
	}

	provider, err := togetherai.New(togetherai.Config{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("create togetherai provider: %w", err)
	}

	ctx := context.Background()
	resp, err := core.GenerateImage(ctx, provider, image.GenerateImageRequest{
		Model:  "black-forest-labs/FLUX.1-schnell",
		Prompt: "A serene mountain lake at sunset with pine trees reflecting in the water",
		N:      1,
	})
	if err != nil {
		return fmt.Errorf("generate image: %w", err)
	}

	if err := os.MkdirAll("out", 0o755); err != nil {
		return err
	}
	for i, img := range resp.Images {
		if img.URL != "" {
			fmt.Printf("Image %d: %s\n", i+1, img.URL)
		}
		if img.Base64 != "" {
			filename := fmt.Sprintf("out/output_%d.png", i+1)
			if err := os.WriteFile(filename, []byte(img.Base64), 0o644); err != nil {
				return err
			}
			fmt.Printf("Saved: %s\n", filename)
		}
	}
	return nil
}

func printDocs() {
	fmt.Println("Image Generation API — usage:")
	fmt.Println("  TOGETHER_API_KEY=sk-... go run ./ai-sdk-examples/image-generation/")
	fmt.Println()
	fmt.Println("Providers implementing image.Provider:")
	fmt.Println("  - pkg/provider/azure/   — Azure OpenAI (AZURE_API_KEY + AZURE_ENDPOINT)")
	fmt.Println("  - pkg/provider/togetherai/ — TogetherAI (TOGETHER_API_KEY)")
	fmt.Println("  - pkg/provider/xai/     — xAI (XAI_API_KEY)")
	fmt.Println()
	fmt.Println("API pattern:")
	fmt.Println("  resp, err := core.GenerateImage(ctx, provider, image.GenerateImageRequest{")
	fmt.Println("      Model: \"black-forest-labs/FLUX.1-schnell\",")
	fmt.Println("      Prompt: \"...\",")
	fmt.Println("      N: 1,")
	fmt.Println("  })")
	fmt.Println("  // resp.Images[0].URL / resp.Images[0].Base64")
}
