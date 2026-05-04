// Command production-setup demonstrates a production-ready provider
// pipeline using middleware: telemetry tracing, automatic retry with
// exponential backoff, and circuit breaker protection.
//
//	Usage:
//	  OPENAI_API_KEY=sk-... go run ./ai-sdk-examples/production-setup/ "What is the capital of France?"
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/samcharles93/ai-sdk/pkg/core"
	"github.com/samcharles93/ai-sdk/pkg/middleware"
	"github.com/samcharles93/ai-sdk/pkg/provider/openai"
	"github.com/samcharles93/ai-sdk/pkg/telemetry"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: production-setup <prompt>")
	}
	prompt := os.Args[1]

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}

	// Step 1: Create the base provider.
	base, err := openai.New(openai.Config{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	// Pipeline: Telemetry → Retry → CircuitBreaker → Provider
	tracer := telemetry.DefaultTracer

	retryCfg := middleware.RetryConfig{MaxAttempts: 3}
	backoff := middleware.ExponentialBackoff{
		BaseDelay:  1 * time.Second,
		MaxDelay:   30 * time.Second,
		Multiplier: 2.0,
		Jitter:     0.5,
	}

	cbCfg := middleware.CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
	}

	// ChainChat composes ChatMiddleware left-to-right. The first
	// middleware becomes outermost; the last is closest to the provider.
	// TelemetryMiddleware is a concrete chat.Provider wrapper, so it
	// sits outside the chain.
	chain := middleware.ChainChat(
		middleware.RetryChat(retryCfg, backoff, middleware.DefaultRetryableError),
		middleware.CircuitBreakerChat(cbCfg),
	)

	wrapped := chain(base)
	finalProvider := middleware.NewTelemetryMiddleware(wrapped, tracer)

	ctx := context.Background()
	result, err := core.GenerateText(ctx, finalProvider, core.GenerateOptions{
		Model:  "gpt-4o",
		Prompt: prompt,
	})
	if err != nil {
		return fmt.Errorf("generate text: %w", err)
	}

	fmt.Printf("Response: %s\n", result.Text)
	fmt.Printf("Finish reason: %s\n", result.FinishReason)
	fmt.Printf("Tokens: %d prompt, %d completion, %d total\n",
		result.TotalUsage.PromptTokens,
		result.TotalUsage.CompletionTokens,
		result.TotalUsage.TotalTokens)

	return nil
}
