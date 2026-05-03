// Command image-generation demonstrates how to use core.GenerateImage
// (or image.Provider.GenerateImage) to create images from text prompts.
//
// This example shows the API pattern for image generation. It currently
// prints usage documentation because providers that implement
// image.Provider (Azure, TogetherAI) require API keys. The pattern is
// shown for reference.
//
//	Usage:
//	  AZURE_API_KEY=... AZURE_ENDPOINT=... go run ./ai-sdk-examples/image-generation/
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/samcharles93/ai-sdk/pkg/core"
	"github.com/samcharles93/ai-sdk/pkg/image"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("error: %v\n", err)
	}
}

func run() error {
	ctx := context.Background()

	// GenerateImageRequest is a provider-agnostic image generation request.
	// It supports model selection, prompt, negative prompt, image count,
	// size/aspect ratio, seed, and provider-specific options.
	req := image.GenerateImageRequest{
		Model:  "dall-e-3",
		Prompt: "A serene mountain lake at sunset with pine trees reflecting in the water",
		Size:   "1024x1024",
		N:      1,
	}

	// When using the Azure provider (pkg/provider/azure/):
	//
	//   provider, err := azure.New(azure.Config{
	//       APIKey:     os.Getenv("AZURE_API_KEY"),
	//       Endpoint:   os.Getenv("AZURE_ENDPOINT"),
	//       Deployment: "dall-e-3",
	//   })
	//   if err != nil { ... }
	//
	//   resp, err := core.GenerateImage(ctx, provider, req)
	//   if err != nil { ... }
	//
	//   for i, img := range resp.Images {
	//       if img.URL != "" {
	//           fmt.Printf("Image %d: %s\n", i, img.URL)
	//       }
	//       if len(img.Data) > 0 {
	//           os.WriteFile(fmt.Sprintf("output_%d.png", i), img.Data, 0o644)
	//       }
	//   }

	_ = core.GenerateImage // suppresses unused import
	_ = ctx
	_ = req
	_ = os.Getenv

	fmt.Println("Image Generation API:")
	fmt.Println("  1. Create an image.Provider implementation")
	fmt.Println("     - pkg/provider/azure/ implements image.Provider for Azure OpenAI")
	fmt.Println("     - pkg/provider/togetherai/ implements image.Provider for TogetherAI")
	fmt.Println("  2. Call core.GenerateImage(ctx, provider, req)")
	fmt.Println("  3. The GenerateImageResponse includes:")
	fmt.Println("     - Images: slice of GeneratedImage (Data, URL, Base64, MediaType)")
	fmt.Println("     - Warnings: non-fatal warnings")
	fmt.Println()
	fmt.Println("The image package (pkg/image/) provides:")
	fmt.Println("  - Provider interface with GenerateImage method")
	fmt.Println("  - GenerateImageRequest/GenerateImageResponse types")
	fmt.Println("  - A thin Client facade with nil-guard")
	fmt.Println("  - Sentinel errors (ErrNoProvider, ErrInvalidRequest)")

	return nil
}
