// Command streaming-chat demonstrates streaming text generation using
// core.StreamText with the OpenAI provider. Text deltas are printed in
// real-time as the model generates them.
//
//	Usage:
//	  OPENAI_API_KEY=sk-... go run ./ai-sdk-examples/streaming-chat/ "Tell me a short story"
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/samcharles93/ai-sdk/core"
	"github.com/samcharles93/ai-sdk/provider/openai"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: streaming-chat <prompt>")
	}
	prompt := os.Args[1]

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}

	provider, err := openai.New(openai.Config{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	ctx := context.Background()

	// StreamText streams text generation in real-time. It returns a
	// StreamResult with TextStream (text-only channel), FullStream
	// (rich event stream), and lazy Usage/FinishReason futures.
	result, err := core.StreamText(ctx, provider, core.GenerateOptions{
		Model:  "gpt-5.4",
		Prompt: prompt,
	})
	if err != nil {
		return fmt.Errorf("stream text: %w", err)
	}

	fmt.Println("Streaming response:")
	fmt.Println("---")

	// Consume text deltas in real-time.
	for delta := range result.TextStream {
		fmt.Print(delta)
	}

	fmt.Println()
	fmt.Println("---")

	// Usage is a future that blocks until the stream completes, then
	// returns the total token usage.
	usage, err := result.Usage()
	if err != nil {
		return fmt.Errorf("get usage: %w", err)
	}
	fmt.Printf("Tokens: %d prompt, %d completion, %d total\n",
		usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)

	reason, err := result.FinishReason()
	if err != nil {
		return fmt.Errorf("get finish reason: %w", err)
	}
	fmt.Printf("Finish reason: %s\n", reason)

	return nil
}
