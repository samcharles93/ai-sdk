// Command headless-toolkit demonstrates the toolkit package running
// fully headless: the model gets read/write/edit/shell/grep/find tools
// confined to a scratch directory, with HeadlessBridge auto-approving
// in place of an interactive UI. This is the substrate for autonomous
// agent loops (agentloop, archie).
//
//	Usage:
//	  ANTHROPIC_API_KEY=sk-ant-... go run ./headless-toolkit/ [model-ref]
//
//	model-ref defaults to "anthropic/claude-sonnet-4-5". The model is
//	asked to create and then verify a file in the scratch directory.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/samcharles93/ai-sdk/core"
	"github.com/samcharles93/ai-sdk/runtime"
	"github.com/samcharles93/ai-sdk/toolkit"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	modelRef := "anthropic/claude-sonnet-4-5"
	if len(os.Args) > 1 {
		modelRef = os.Args[1]
	}

	dir, err := os.MkdirTemp("", "headless-toolkit-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	// The registry's tools are jailed to dir; the HeadlessBridge stands in
	// for a human, auto-approving anything a UI would have prompted for.
	reg := toolkit.NewRegistry()
	if err := toolkit.RegisterBuiltins(reg, dir); err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	tools := reg.CoreToolSet(toolkit.HeadlessBridge{Logger: logger})

	runtime.RegisterBuiltinClasses()
	rt := runtime.NewRuntime(runtime.Config{
		Providers: map[string]runtime.ProviderConfig{
			"anthropic": {
				ID:    "anthropic",
				Class: "anthropic",
				Auth:  runtime.AuthConfig{APIKeyEnv: "ANTHROPIC_API_KEY"},
			},
			"openai": {
				ID:    "openai",
				Class: "openai",
				Auth:  runtime.AuthConfig{APIKeyEnv: "OPENAI_API_KEY"},
			},
			"ollama": {
				ID:    "ollama",
				Class: "ollama",
				Auth:  runtime.AuthConfig{Type: runtime.AuthTypeNone},
			},
		},
	})

	res, err := rt.Chat(context.Background(), modelRef, core.GenerateOptions{
		System: "You are a careful engineering agent working in a scratch directory. Use your tools; do not guess file contents.",
		Prompt: "Create a file named notes.txt containing the single line 'toolkit works'. " +
			"Then read it back with the read tool and run `cat notes.txt` with the shell tool to verify. " +
			"Finish by stating the file content.",
		Tools:    tools,
		MaxSteps: 8,
	})
	if err != nil {
		return fmt.Errorf("chat: %w", err)
	}

	fmt.Println("--- final text ---")
	fmt.Println(res.Text)
	fmt.Printf("--- %d steps, %d total tokens ---\n", len(res.Steps), res.TotalUsage.TotalTokens)

	content, err := os.ReadFile(dir + "/notes.txt")
	if err != nil {
		return fmt.Errorf("the model did not create notes.txt: %w", err)
	}
	fmt.Printf("verified on disk: %q\n", string(content))
	return nil
}
