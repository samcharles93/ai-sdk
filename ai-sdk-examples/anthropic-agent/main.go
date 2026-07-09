// Command anthropic-agent demonstrates an agent with tool use using the
// AI SDK and Anthropic provider. It shows how to define tools, register
// them with agent.RunAgent, and process streaming events.
//
//	Usage:
//	  ANTHROPIC_API_KEY=sk-ant-... go run ./ai-sdk-examples/anthropic-agent/ "What is the weather in London?"
//
//	The agent uses a mock weather tool that returns hard-coded data.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/samcharles93/ai-sdk/agent"
	"github.com/samcharles93/ai-sdk/core"
	"github.com/samcharles93/ai-sdk/provider/anthropic"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: anthropic-agent <prompt>")
	}
	prompt := os.Args[1]

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY environment variable is required")
	}

	// Create the Anthropic provider.
	provider, err := anthropic.New(anthropic.Config{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("create anthropic provider: %w", err)
	}

	// Define a mock weather tool. The Parameters field is a JSON Schema
	// describing the tool's expected input. The Execute function is called
	// when the model requests this tool.
	weatherSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"location": {
				"type": "string",
				"description": "The city name"
			}
		},
		"required": ["location"]
	}`)

	tools := core.ToolSet{
		"get_weather": core.NewTool(
			"get_weather",
			"Get the current weather for a given city",
			weatherSchema,
			func(ctx context.Context, input string) (string, error) {
				// In a real application, you would call a weather API here.
				var args struct {
					Location string `json:"location"`
				}
				if err := json.Unmarshal([]byte(input), &args); err != nil {
					return "", fmt.Errorf("parse arguments: %w", err)
				}
				result := map[string]any{
					"location":    args.Location,
					"temperature": 22,
					"condition":   "sunny",
					"humidity":    45,
				}
				out, _ := json.Marshal(result)
				return string(out), nil
			},
		),
	}

	ctx := context.Background()

	// RunAgent orchestrates a multi-step agent run. It wraps core.StreamText
	// and translates core.StreamPart events into agent.StreamEvent values
	// on the returned channel.
	events, err := agent.RunAgent(ctx, provider, prompt, tools, 5)
	if err != nil {
		return fmt.Errorf("run agent: %w", err)
	}

	fmt.Println("Agent started. Streaming output:")
	fmt.Println("---")

	// Consume streaming events until the channel closes.
	for ev := range events {
		switch ev.Type {
		case agent.EventTextDelta:
			fmt.Print(ev.TextDelta)
		case agent.EventReasoningDelta:
			fmt.Printf("[thinking: %s]", ev.ReasoningDelta)
		case agent.EventToolCall:
			fmt.Printf("\n🔧 Calling tool: %s\n   Input: %s\n", ev.ToolCall.ToolName, ev.ToolCall.Input)
		case agent.EventToolResult:
			fmt.Printf("📋 Tool result: %s\n", ev.ToolResult.Output)
		case agent.EventStartStep:
			fmt.Printf("\n--- Step start ---\n")
		case agent.EventFinishStep:
			if ev.StepResult != nil {
				fmt.Printf("\n--- Step finish (reason: %s) ---\n", ev.StepResult.FinishReason)
			}
		case agent.EventFinish:
			fmt.Printf("\n\nDone. Finish reason: %s\n", ev.FinishReason)
			if ev.TotalUsage != nil {
				fmt.Printf("Tokens: %d prompt, %d completion, %d total\n",
					ev.TotalUsage.PromptTokens,
					ev.TotalUsage.CompletionTokens,
					ev.TotalUsage.TotalTokens)
			}
		case agent.EventError:
			fmt.Printf("\n❌ Error: %v\n", ev.Error)
		case agent.EventAbort:
			fmt.Printf("\n⚠️  Aborted: %v\n", ev.Error)
		}
	}

	return nil
}
