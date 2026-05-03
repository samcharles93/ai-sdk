// Command ai-sdk is the composition root for the AI SDK chat application.
// It wires together all providers, the registry, and HTTP handlers, then
// starts an HTTP server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/samcharles93/ai-sdk/pkg/provider/anthropic"
	"github.com/samcharles93/ai-sdk/pkg/provider/azure"
	"github.com/samcharles93/ai-sdk/pkg/provider/cohere"
	"github.com/samcharles93/ai-sdk/pkg/provider/deepseek"
	"github.com/samcharles93/ai-sdk/pkg/provider/gemini"
	"github.com/samcharles93/ai-sdk/pkg/provider/groq"
	"github.com/samcharles93/ai-sdk/pkg/provider/mistral"
	"github.com/samcharles93/ai-sdk/pkg/provider/ollama"
	"github.com/samcharles93/ai-sdk/pkg/provider/openai"
	"github.com/samcharles93/ai-sdk/pkg/provider/perplexity"
	"github.com/samcharles93/ai-sdk/pkg/provider/xai"
	"github.com/samcharles93/ai-sdk/pkg/registry"
	"github.com/samcharles93/ai-sdk/pkg/ui/handlers"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	port := flag.Int("port", 8080, "HTTP server port")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	reg := registry.New()

	registerProviders(reg)

	mux := http.NewServeMux()

	chatHandler := handlers.NewChatHandler(reg, "openai", "gpt-4o")
	mux.Handle("/chat", chatHandler)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("listen and serve: %w", err)
	case <-ctx.Done():
		logger.Info("shutting down gracefully")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("server stopped")
	return nil
}

func registerProviders(reg *registry.Registry) {
	providers := []struct {
		name string
		fn   func() error
	}{
		{"openai", func() error { return registerOpenAI(reg) }},
		{"anthropic", func() error { return registerAnthropic(reg) }},
		{"deepseek", func() error { return registerDeepSeek(reg) }},
		{"gemini", func() error { return registerGemini(reg) }},
		{"ollama", func() error { registerOllama(reg); return nil }},
		{"mistral", func() error { return registerMistral(reg) }},
		{"groq", func() error { return registerGroq(reg) }},
		{"xai", func() error { return registerXAI(reg) }},
		{"perplexity", func() error { return registerPerplexity(reg) }},
		{"azure", func() error { return registerAzure(reg) }},
		{"cohere", func() error { return registerCohere(reg) }},
	}

	logger := slog.Default()
	for _, p := range providers {
		if err := p.fn(); err != nil {
			logger.Info("provider skipped", "name", p.name, "reason", err)
			continue
		}
		logger.Info("provider registered", "name", p.name)
	}
}

func registerOpenAI(reg *registry.Registry) error {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return fmt.Errorf("OPENAI_API_KEY not set")
	}
	p, err := openai.New(openai.Config{APIKey: key})
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}
	reg.RegisterChat("openai", p)
	return nil
}

func registerAnthropic(reg *registry.Registry) error {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	p, err := anthropic.New(anthropic.Config{APIKey: key})
	if err != nil {
		return fmt.Errorf("create anthropic provider: %w", err)
	}
	reg.RegisterChat("anthropic", p)
	return nil
}

func registerDeepSeek(reg *registry.Registry) error {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		return fmt.Errorf("DEEPSEEK_API_KEY not set")
	}
	p, err := deepseek.New(deepseek.Config{APIKey: key})
	if err != nil {
		return fmt.Errorf("create deepseek provider: %w", err)
	}
	reg.RegisterChat("deepseek", p)
	return nil
}

func registerGemini(reg *registry.Registry) error {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		return fmt.Errorf("GEMINI_API_KEY not set")
	}
	p, err := gemini.New(gemini.Config{APIKey: key})
	if err != nil {
		return fmt.Errorf("create gemini provider: %w", err)
	}
	reg.RegisterChat("gemini", p)
	return nil
}

func registerOllama(reg *registry.Registry) {
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	p := ollama.New(ollama.Config{BaseURL: baseURL})
	reg.RegisterChat("ollama", p)
}

func registerMistral(reg *registry.Registry) error {
	key := os.Getenv("MISTRAL_API_KEY")
	if key == "" {
		return fmt.Errorf("MISTRAL_API_KEY not set")
	}
	p, err := mistral.New(mistral.Config{APIKey: key})
	if err != nil {
		return fmt.Errorf("create mistral provider: %w", err)
	}
	reg.RegisterChat("mistral", p)
	reg.RegisterEmbed("mistral", p)
	return nil
}

func registerGroq(reg *registry.Registry) error {
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		return fmt.Errorf("GROQ_API_KEY not set")
	}
	p, err := groq.New(groq.Config{APIKey: key})
	if err != nil {
		return fmt.Errorf("create groq provider: %w", err)
	}
	reg.RegisterChat("groq", p)
	return nil
}

func registerXAI(reg *registry.Registry) error {
	key := os.Getenv("XAI_API_KEY")
	if key == "" {
		return fmt.Errorf("XAI_API_KEY not set")
	}
	p, err := xai.New(xai.Config{APIKey: key})
	if err != nil {
		return fmt.Errorf("create xai provider: %w", err)
	}
	reg.RegisterChat("xai", p)
	return nil
}

func registerPerplexity(reg *registry.Registry) error {
	key := os.Getenv("PERPLEXITY_API_KEY")
	if key == "" {
		return fmt.Errorf("PERPLEXITY_API_KEY not set")
	}
	p, err := perplexity.New(perplexity.Config{APIKey: key})
	if err != nil {
		return fmt.Errorf("create perplexity provider: %w", err)
	}
	reg.RegisterChat("perplexity", p)
	return nil
}

func registerAzure(reg *registry.Registry) error {
	key := os.Getenv("AZURE_OPENAI_API_KEY")
	endpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	deployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
	if key == "" || endpoint == "" || deployment == "" {
		return fmt.Errorf("azure config not set")
	}
	p, err := azure.New(azure.Config{APIKey: key, Endpoint: endpoint, Deployment: deployment})
	if err != nil {
		return fmt.Errorf("create azure provider: %w", err)
	}
	reg.RegisterChat("azure", p)
	reg.RegisterEmbed("azure", p)
	reg.RegisterImage("azure", p)
	return nil
}

func registerCohere(reg *registry.Registry) error {
	key := os.Getenv("COHERE_API_KEY")
	if key == "" {
		return fmt.Errorf("COHERE_API_KEY not set")
	}
	p, err := cohere.New(cohere.Config{APIKey: key})
	if err != nil {
		return fmt.Errorf("create cohere provider: %w", err)
	}
	reg.RegisterChat("cohere", p)
	reg.RegisterEmbed("cohere", p)
	reg.RegisterRerank("cohere", p)
	return nil
}
