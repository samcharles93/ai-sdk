// Command openai-chat demonstrates an interactive streaming chat with
// tool use, reasoning display, and real-time text output. It builds on
// core.StreamText and shows the full capabilities of the AI SDK.
//
//	Usage:
//	  OPENAI_API_KEY=sk-... go run ./ai-sdk-examples/openai-chat/
//
//	Enter prompts on stdin. Type "exit" to quit. The chat includes a
//	"time" tool that returns the current server time.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
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

	provider, err := openai.New(openai.Config{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	model := "gpt-5.4-nano"

	tools := core.ToolSet{
		"get_time": core.NewTool(
			"get_time",
			"Get the current date and time",
			json.RawMessage(`{"type":"object","properties":{},"required":[]}`),
			func(ctx context.Context, input string) (string, error) {
				return time.Now().Format(time.RFC1123), nil
			},
		),
	}

	ctx := context.Background()

	fmt.Printf("AI SDK Chat (%s)\n", model)
	fmt.Println("Supports: streaming, reasoning, tool calls (try \"what time is it?\")")
	fmt.Println("Type \"exit\" to quit.")
	fmt.Println(strings.Repeat("—", 60))

	scanner := bufio.NewScanner(os.Stdin)
	messages := make([]chat.Message, 0)
	for {
		fmt.Print("\n> ")
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

		messages = append(messages, chat.Message{Role: chat.RoleUser, Content: prompt})
		result, err := core.StreamText(ctx, provider, core.GenerateOptions{
			Model:    model,
			Messages: messages,
			Tools:    tools,
			MaxSteps: 5,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			messages = messages[:len(messages)-1]
			continue
		}

		var reply strings.Builder
		for part := range result.FullStream {
			switch part.Type {
			case core.StreamPartReasoningDelta:
				if reply.Len() == 0 {
					fmt.Print("\x1b[2m")
				}
				fmt.Print(part.ReasoningDelta)

			case core.StreamPartTextDelta:
				if reply.Len() == 0 {
					fmt.Print("\x1b[0m\n")
				}
				fmt.Print(part.TextDelta)
				reply.WriteString(part.TextDelta)

			case core.StreamPartToolCall:
				fmt.Printf("\n\x1b[33m🔧 tool: %s(%s)\x1b[0m\n",
					part.ToolCall.ToolName, part.ToolCall.Input)

			case core.StreamPartToolResult:
				fmt.Printf("   → %s\n", part.ToolResult.Output)

			case core.StreamPartFinish:
				if part.TotalUsage != nil {
					fmt.Printf("\n\x1b[90m(%d tokens)\x1b[0m\n",
						part.TotalUsage.TotalTokens)
				}

			case core.StreamPartError:
				fmt.Fprintf(os.Stderr, "\nstream error: %v\n", part.Error)
			}
		}
		fmt.Println()

		if reply.Len() > 0 {
			messages = append(messages, chat.Message{Role: chat.RoleAssistant, Content: reply.String()})
		}
	}

	return nil
}
