package agent

import (
	"context"
	"encoding/json"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
)

// defaultSubagentMaxSteps bounds the sub-agent's tool loop when the caller
// does not set one. Sub-agents are short, self-contained delegations; a
// generous ceiling is fine because each step is a normal model call.
const defaultSubagentMaxSteps = 10

// Subagent configures a nested agent run for a subtask. It executes in a
// fresh context window — the parent's history is never sent — and returns
// the sub-agent's final text, so a parent agent can delegate a
// self-contained piece of work and get a conclusion back without growing
// its own context.
//
// This is the shared-process form of multi-agent delegation (a nested
// [core.GenerateText]), not a child process: no wire protocol, no process
// lifecycle. Callers that need OS-level isolation should look elsewhere;
// the SDK deliberately does not own process spawning.
type Subagent struct {
	// Provider and Model select the model the sub-agent runs on.
	Provider chat.Provider
	Model    string
	// System is an optional system prompt for the sub-agent.
	System string
	// Tools is the toolset the sub-agent may call. Nil means the
	// sub-agent is analysis-only. Callers should pass a distinct (typically
	// read-only) toolset, not the parent's, to avoid unbounded recursion
	// when the parent also registers this sub-agent tool.
	Tools core.ToolSet
	// MaxSteps bounds the sub-agent's tool-loop iterations. Defaults to
	// defaultSubagentMaxSteps when unset.
	MaxSteps int
	// MaxTokens bounds the sub-agent's total output tokens. Zero is
	// unbounded.
	MaxTokens int
}

// Run executes the subtask and returns the sub-agent's final text.
func (s Subagent) Run(ctx context.Context, prompt string) (string, error) {
	maxSteps := s.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultSubagentMaxSteps
	}
	res, err := core.GenerateText(ctx, s.Provider, core.GenerateOptions{
		Model:     s.Model,
		System:    s.System,
		Prompt:    prompt,
		Tools:     s.Tools,
		MaxSteps:  maxSteps,
		MaxTokens: s.MaxTokens,
	})
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

// Tool returns a [core.Tool] the parent agent can register (typically under
// the name "subagent"). Its argument is a single "prompt": the self-contained
// subtask to delegate.
func (s Subagent) Tool() *core.Tool {
	params := json.RawMessage(`{
		"type": "object",
		"properties": {
			"prompt": {
				"type": "string",
				"description": "The self-contained subtask to delegate. The sub-agent runs it in a fresh context window and returns its conclusion."
			}
		},
		"required": ["prompt"]
	}`)
	return core.NewTool("subagent",
		"Delegate a self-contained subtask to a nested agent that works in a fresh context window and returns its conclusion. Use for analysis or synthesis that would otherwise bloat this conversation.",
		params,
		func(ctx context.Context, input string) (string, error) {
			var p struct {
				Prompt string `json:"prompt"`
			}
			if err := json.Unmarshal([]byte(input), &p); err != nil {
				//nolint:nilerr // Malformed arguments are reported to the model in-band so it can retry; a Go error would abort the parent run.
				return "subagent rejected: invalid arguments: " + err.Error(), nil
			}
			if p.Prompt == "" {
				return "subagent rejected: prompt is required", nil
			}
			return s.Run(ctx, p.Prompt)
		})
}
