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
	"github.com/samcharles93/ai-sdk/pkg/provider/deepseek"
	"github.com/samcharles93/ai-sdk/pkg/provider/gemini"
	"github.com/samcharles93/ai-sdk/pkg/provider/ollama"
	"github.com/samcharles93/ai-sdk/pkg/provider/openai"
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
