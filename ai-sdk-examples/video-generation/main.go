// Command video-generation demonstrates how to use core.GenerateVideo
// (or video.Provider.GenerateVideo) to create videos from text prompts.
//
// This example shows the API pattern for video generation. It currently
// prints usage documentation because the xAI provider that implements
// video.Provider requires an API key and video generation is an expensive
// and slow operation.
//
//	Usage:
//	  XAI_API_KEY=... go run ./ai-sdk-examples/video-generation/
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/samcharles93/ai-sdk/pkg/core"
	"github.com/samcharles93/ai-sdk/pkg/video"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("error: %v\n", err)
	}
}

func run() error {
	ctx := context.Background()

	// GenerateVideoRequest is a provider-agnostic video generation request.
	// It supports model selection, prompt, duration, resolution, frame rate,
	// and provider-specific options.
	req := video.GenerateVideoRequest{
		Model:      "grok-video",
		Prompt:     "A serene mountain lake at sunset with gentle ripples on the water and birds flying across the sky",
		Duration:   "00:00:05",
		Resolution: "1920x1080",
		FrameRate:  24,
	}

	// When using the xAI provider (pkg/provider/xai/):
	//
	//   provider, err := xai.New(xai.Config{
	//       APIKey: os.Getenv("XAI_API_KEY"),
	//   })
	//   if err != nil { ... }
	//
	//   resp, err := core.GenerateVideo(ctx, provider, req)
	//   if err != nil { ... }
	//
	//   for i, vid := range resp.Videos {
	//       if vid.URL != "" {
	//           fmt.Printf("Video %d: %s\n", i, vid.URL)
	//       }
	//       if len(vid.Data) > 0 {
	//           os.WriteFile(fmt.Sprintf("output_%d.mp4", i), vid.Data, 0o644)
	//       }
	//   }

	_ = core.GenerateVideo // suppresses unused import
	_ = ctx
	_ = req
	_ = os.Getenv

	fmt.Println("Video Generation API:")
	fmt.Println("  1. Create a video.Provider implementation")
	fmt.Println("     - pkg/provider/xai/ implements video.Provider for xAI grok-video")
	fmt.Println("  2. Call core.GenerateVideo(ctx, provider, req)")
	fmt.Println("  3. The GenerateVideoResponse includes:")
	fmt.Println("     - Videos: slice of VideoResult (Data, URL, MediaType)")
	fmt.Println("     - Warnings: non-fatal warnings")
	fmt.Println()
	fmt.Println("The video package (pkg/video/) provides:")
	fmt.Println("  - Provider interface with GenerateVideo method")
	fmt.Println("  - GenerateVideoRequest / GenerateVideoResponse types")
	fmt.Println("  - A thin Client facade with nil-guard")
	fmt.Println("  - Sentinel errors (ErrNoProvider, ErrInvalidRequest)")

	return nil
}
