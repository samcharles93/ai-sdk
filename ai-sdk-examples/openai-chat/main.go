// Command openai-chat demonstrates a simple chat CLI using the AI SDK
// with the OpenAI provider. It reads user input line-by-line from stdin,
// sends each line as a prompt to the model via core.GenerateText, and
// prints the response.
//
//	Usage:
//	  OPENAI_API_KEY=sk-... go run ./ai-sdk-examples/openai-chat/
//
//	Enter prompts on stdin (one per line). Press Ctrl+D or type "exit" to quit.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/samcharles93/ai-sdk/pkg/core"
	"github.com/samcharles93/ai-sdk/pkg/provider/openai"
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

	// Create the OpenAI provider. This is a chat.Provider that speaks
	// the OpenAI wire format directly using only the Go standard library.
	provider, err := openai.New(openai.Config{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	ctx := context.Background()
	model := "gpt-5.4-nano"

	fmt.Printf("OpenAI Chat (%s)\n", model)
	fmt.Println("Enter prompts (one per line). Type \"exit\" or press Ctrl+D to quit.")
	fmt.Println(strings.Repeat("-", 50))

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		prompt := strings.TrimSpace(scanner.Text())
		if prompt == "" {
			continue
		}
		if strings.EqualFold(prompt, "exit") || strings.EqualFold(prompt, "quit") {
			break
		}

		// GenerateText orchestrates a non-streaming text generation with
		// the given provider and options. It handles the tool-call loop
		// internally if tools are configured.
		result, err := core.GenerateText(ctx, provider, core.GenerateOptions{
			Model:  model,
			Prompt: prompt,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}

		if result.Reasoning != "" {
			fmt.Print("\x1b[2m") // dim
			fmt.Println(result.Reasoning)
			fmt.Print("\x1b[0m") // reset
			fmt.Println(strings.Repeat("—", 40))
		}
		fmt.Println(result.Text)
		fmt.Println()
	}

	return nil
}
