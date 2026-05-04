// Command multi-provider demonstrates registering multiple providers
// via the registry and switching between them for different model
// capabilities (chat, embedding, reranking).
//
//	Usage:
//	  OPENAI_API_KEY=sk-... COHERE_API_KEY=... go run ./ai-sdk-examples/multi-provider/
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/samcharles93/ai-sdk/pkg/chat"
	"github.com/samcharles93/ai-sdk/pkg/embed"
	"github.com/samcharles93/ai-sdk/pkg/provider/cohere"
	"github.com/samcharles93/ai-sdk/pkg/provider/openai"
	"github.com/samcharles93/ai-sdk/pkg/registry"
	"github.com/samcharles93/ai-sdk/pkg/rerank"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}
	cohereKey := os.Getenv("COHERE_API_KEY")
	if cohereKey == "" {
		return fmt.Errorf("COHERE_API_KEY environment variable is required")
	}

	openaiProv, err := openai.New(openai.Config{APIKey: openaiKey})
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	cohereProv, err := cohere.New(cohere.Config{APIKey: cohereKey})
	if err != nil {
		return fmt.Errorf("create cohere provider: %w", err)
	}

	reg := registry.New()
	reg.RegisterChat("openai", openaiProv)
	reg.RegisterEmbed("openai", openaiProv)
	reg.RegisterEmbed("cohere", cohereProv)
	reg.RegisterRerank("cohere", cohereProv)

	ctx := context.Background()

	fmt.Println("=== Embedding (OpenAI) ===")
	embedClient, err := reg.Embed("openai")
	if err != nil {
		return fmt.Errorf("get openai embed client: %w", err)
	}
	embResp, err := embedClient.Embed(ctx, embed.Request{
		Model:  "text-embedding-3-small",
		Inputs: []string{"Hello, world!"},
	})
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	fmt.Printf("  Dimensions: %d\n", len(embResp.Embeddings[0].Vector))
	fmt.Printf("  Tokens:     %d prompt / %d total\n",
		embResp.Usage.PromptTokens, embResp.Usage.TotalTokens)

	fmt.Println("\n=== Embedding (Cohere) ===")
	cohereEmbedClient, err := reg.Embed("cohere")
	if err != nil {
		return fmt.Errorf("get cohere embed client: %w", err)
	}
	cohereEmbResp, err := cohereEmbedClient.Embed(ctx, embed.Request{
		Model:  "embed-english-v3.0",
		Inputs: []string{"Multi-provider AI SDK example"},
	})
	if err != nil {
		return fmt.Errorf("cohere embed: %w", err)
	}
	fmt.Printf("  Dimensions: %d\n", len(cohereEmbResp.Embeddings[0].Vector))
	fmt.Printf("  Tokens:     %d prompt / %d total\n",
		cohereEmbResp.Usage.PromptTokens, cohereEmbResp.Usage.TotalTokens)

	fmt.Println("\n=== Reranking (Cohere) ===")
	rerankClient, err := reg.Rerank("cohere")
	if err != nil {
		return fmt.Errorf("get cohere rerank client: %w", err)
	}
	documents := []string{
		"Go compiles to static binaries for simple deployment.",
		"Docker is useful for containerizing Python applications.",
		"Go's goroutines provide lightweight concurrency for high-throughput services.",
	}
	rerankResp, err := rerankClient.Rerank(ctx, rerank.Request{
		Model:     "rerank-english-v3.0",
		Query:     "Go language features and deployment",
		Documents: documents,
		TopN:      2,
	})
	if err != nil {
		return fmt.Errorf("rerank: %w", err)
	}
	for i, item := range rerankResp.Ranking {
		fmt.Printf("  %d. Score: %.4f — %s\n", i+1, item.Score, item.Document)
	}

	fmt.Println("\n=== Chat (OpenAI) ===")
	chatClient, err := reg.Chat("openai")
	if err != nil {
		return fmt.Errorf("get openai chat client: %w", err)
	}
	chatResp, err := chatClient.Chat(ctx, chat.Request{
		Model: "gpt-4o",
		Messages: []chat.Message{
			{Role: chat.RoleUser, Content: "What is Go best known for? Answer in one sentence."},
		},
	})
	if err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	content := chatResp.Content
	if len(content) > 120 {
		content = content[:120] + "..."
	}
	fmt.Printf("  Response: %s\n", content)
	fmt.Printf("  Tokens:   %d prompt / %d completion / %d total\n",
		chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, chatResp.Usage.TotalTokens)

	return nil
}
