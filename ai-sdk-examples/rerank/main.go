// Command rerank demonstrates document reranking using the AI SDK with
// the Cohere provider. Reranking reorders documents by relevance to a
// query, which is essential for retrieval-augmented generation (RAG)
// and search quality improvements.
//
//	Usage:
//	  COHERE_API_KEY=... go run ./ai-sdk-examples/rerank/
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/samcharles93/ai-sdk/pkg/provider/cohere"
	"github.com/samcharles93/ai-sdk/pkg/rerank"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	apiKey := os.Getenv("COHERE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("COHERE_API_KEY environment variable is required")
	}

	// Cohere Provider implements chat.Provider, embed.Provider, and
	// rerank.Provider — a single struct for all three capabilities.
	provider, err := cohere.New(cohere.Config{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("create cohere provider: %w", err)
	}

	ctx := context.Background()

	// Documents to rerank — in a real RAG pipeline these would be
	// retrieval results from a vector search.
	query := "What are the best practices for deploying Go applications in production?"

	documents := []string{
		"Go applications compile to single static binaries, making deployment simple.",
		"Docker is a popular container platform that can run Go applications.",
		"The best production deployment for Go uses multi-stage Docker builds with Alpine Linux for small image sizes.",
		"Cloud providers like AWS, GCP, and Azure all support Go applications natively.",
		"Kubernetes is an open-source container orchestration platform for automating deployment and scaling.",
		"Go's built-in HTTP server is production-ready and does not require a reverse proxy for most workloads.",
		"You should use environment variables and configuration files for managing settings across environments.",
		"Recent advances in AI have led to new deployment patterns for machine learning models.",
	}

	req := rerank.Request{
		Model:     "rerank-english-v3.0",
		Query:     query,
		Documents: documents,
		TopN:      3, // return only the top 3 most relevant documents
	}

	resp, err := provider.Rerank(ctx, req)
	if err != nil {
		return fmt.Errorf("rerank: %w", err)
	}

	fmt.Printf("Query: %s\n", query)
	fmt.Printf("Model: %s\n", resp.Model)
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("Ranked results:")

	for i, item := range resp.Ranking {
		fmt.Printf("\n%d. Score: %.4f (original index: %d)\n", i+1, item.Score, item.OriginalIndex)
		fmt.Printf("   %s\n", item.Document)
	}

	fmt.Println()
	fmt.Println("The documents are ordered by relevance to the query, with the")
	fmt.Println("most relevant deployment best-practice ranked first.")

	return nil
}
