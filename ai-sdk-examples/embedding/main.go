// Command embedding demonstrates how to generate embeddings and compute
// vector similarity using the AI SDK with the OpenAI provider.
//
//	Usage:
//	  OPENAI_API_KEY=sk-... go run ./ai-sdk-examples/embedding/
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/samcharles93/ai-sdk/embed"
	"github.com/samcharles93/ai-sdk/provider/openai"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}

	// Create the OpenAI provider. The same struct implements both
	// chat.Provider and embed.Provider, so we can use it directly
	// for embedding calls.
	provider, err := openai.New(openai.Config{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	ctx := context.Background()

	// Embedding request with two inputs for similarity comparison.
	req := embed.Request{
		Model: "text-embedding-3-small",
		Inputs: []string{
			"The cat sat on the mat",
			"The feline rested on the rug",
		},
	}

	resp, err := provider.Embed(ctx, req)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}

	// Verify we got both embeddings back.
	if len(resp.Embeddings) != 2 {
		return fmt.Errorf("expected 2 embeddings, got %d", len(resp.Embeddings))
	}

	e1 := resp.Embeddings[0].Vector
	e2 := resp.Embeddings[1].Vector

	fmt.Printf("Model: %s\n", resp.Model)
	fmt.Printf("Embedding dimensions: %d\n", len(e1))
	fmt.Printf("Tokens used: %d prompt, %d total\n",
		resp.Usage.PromptTokens, resp.Usage.TotalTokens)

	// Compute cosine similarity between the two embeddings.
	similarity, err := embed.CosineSimilarity(e1, e2)
	if err != nil {
		return fmt.Errorf("cosine similarity: %w", err)
	}
	fmt.Printf("Cosine similarity: %.4f\n", similarity)

	// Also show dot product and vector norms.
	dot, _ := embed.DotProduct(e1, e2)
	fmt.Printf("Dot product: %.4f\n", dot)
	fmt.Printf("Vector norms: %.4f, %.4f\n", embed.Norm(e1), embed.Norm(e2))

	fmt.Println()
	fmt.Println("The high similarity score confirms the two sentences are")
	fmt.Println("semantically close despite different word choices.")

	return nil
}
