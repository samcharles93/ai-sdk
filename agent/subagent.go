package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
)

// defaultSubagentMaxSteps bounds the sub-agent's tool loop when the caller
// does not set one. A sub-agent is a self-contained delegation that may need
// several tool iterations, so this is larger than Agent's single-step
// default; it is still bounded so a misbehaving sub-task cannot run away.
const defaultSubagentMaxSteps = 10

// maxSubagentDepth caps how deeply one sub-agent can nest another. It turns
// the "pass a distinct toolset" advice below into a hard backstop: if a
// caller shares a toolset that still contains a sub-agent tool, recursion
// fails fast here rather than spawning unbounded nested generations.
const maxSubagentDepth = 5

// subagentDepthKey carries the nesting depth through the nested tool loop via
// context, so a sub-agent's own sub-agent tool call sees the incremented
// depth.
type subagentDepthKey struct{}

// Subagent configures a nested agent run for a subtask. It executes in a
// fresh context window — the parent's history is never sent — and returns
// the sub-agent's final text, so a parent agent can delegate a
// self-contained piece of work and get a conclusion back without growing
// its own context.
//
// This is the shared-process form of multi-agent delegation (a nested
// [core.GenerateText]), not a child process: no wire protocol, no process
// lifecycle. Callers that need OS-level isolation should look elsewhere; the
// SDK deliberately does not own process spawning.
//
// The nested run is synchronous and non-streaming: nothing is emitted to the
// parent between the tool-call and tool-result events, so a long delegation
// reads as a single in-progress tool call.
type Subagent struct {
	// Provider and Model select the model the sub-agent runs on.
	Provider chat.Provider
	Model    string
	// System is an optional system prompt for the sub-agent.
	System string
	// Tools is the toolset the sub-agent may call. Nil means the
	// sub-agent is analysis-only. Callers should pass a distinct (typically
	// read-only) toolset, not the parent's, to avoid recursion; maxSubagentDepth
	// backstops this if they do not.
	Tools core.ToolSet
	// MaxSteps bounds the sub-agent's tool-loop iterations. Defaults to
	// defaultSubagentMaxSteps when unset.
	MaxSteps int
	// MaxTokens is the per-call output token ceiling passed to each model
	// call in the sub-agent's loop. It is not a cumulative budget: a
	// multi-step sub-agent can emit up to MaxTokens per step. Zero is
	// unbounded.
	MaxTokens int
	// Temperature controls sampling for the sub-agent's calls. Zero means
	// the provider default.
	Temperature float32
	// ProviderOptions carries provider-specific options (e.g. Anthropic
	// reasoning_effort) for the sub-agent's calls, mirroring
	// [chat.Request.ProviderOptions]. Nil means none.
	ProviderOptions map[string]any
	// Name is the tool name used by [Subagent.Tool]. Empty defaults to
	// "subagent"; set it to register multiple distinct sub-agents.
	Name string
}

// Run executes the subtask and returns the sub-agent's final text.
func (s Subagent) Run(ctx context.Context, prompt string) (string, error) {
	depth, _ := ctx.Value(subagentDepthKey{}).(int)
	if depth >= maxSubagentDepth {
		return "", fmt.Errorf("sub-agent nesting depth %d exceeds limit %d", depth, maxSubagentDepth)
	}
	ctx = context.WithValue(ctx, subagentDepthKey{}, depth+1)

	maxSteps := s.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultSubagentMaxSteps
	}

	res, err := core.GenerateText(ctx, s.Provider, core.GenerateOptions{
		Model:           s.Model,
		System:          s.System,
		Prompt:          prompt,
		Tools:           s.Tools,
		MaxSteps:        maxSteps,
		MaxTokens:       s.MaxTokens,
		Temperature:     s.Temperature,
		ProviderOptions: s.ProviderOptions,
	})
	if err != nil {
		return "", err
	}

	// A sub-agent's value is its conclusion. Surface the cases where the
	// loop ended without one rather than returning ("", nil), which a parent
	// cannot distinguish from a silent failure.
	switch res.FinishReason {
	case core.FinishReasonToolCalls:
		return "", fmt.Errorf("sub-agent reached its step limit (%d) with an unfinished tool call", maxSteps)
	case core.FinishReasonLength:
		return "", errors.New("sub-agent output was truncated by the token limit")
	case core.FinishReasonContentFilter:
		return "", errors.New("sub-agent output was blocked by the content filter")
	case core.FinishReasonError:
		return "", errors.New("sub-agent ended with an error")
	}
	if strings.TrimSpace(res.Text) == "" {
		return "", errors.New("sub-agent produced no conclusion")
	}
	return res.Text, nil
}

// Tool returns a [core.Tool] the parent agent can register. Its argument is
// a single "prompt": the self-contained subtask to delegate.
//
// The parent must allow at least two steps for the delegation to be useful:
// one step runs the sub-agent tool, a second synthesises its conclusion into
// the parent's final answer. A parent configured for a single step (Agent's
// default) breaks before that synthesis.
func (s Subagent) Tool() *core.Tool {
	name := s.Name
	if name == "" {
		name = "subagent"
	}
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
	return core.NewTool(name,
		"Delegate a self-contained subtask to a nested agent that works in a fresh context window and returns its conclusion. Use for analysis or synthesis that would otherwise bloat this conversation.",
		params,
		func(ctx context.Context, input string) (string, error) {
			var p struct {
				Prompt string `json:"prompt"`
			}
			if err := json.Unmarshal([]byte(input), &p); err != nil {
				//nolint:nilerr // The unmarshal error is folded into the in-band rejection so the model sees a clear, retryable message; returning it as a Go error would surface a bare error string instead.
				return "subagent rejected: invalid arguments: " + err.Error(), nil
			}
			if strings.TrimSpace(p.Prompt) == "" {
				return "subagent rejected: prompt is required", nil
			}
			out, err := s.Run(ctx, p.Prompt)
			if err != nil {
				return "", fmt.Errorf("sub-agent failed: %w", err)
			}
			return out, nil
		})
}
