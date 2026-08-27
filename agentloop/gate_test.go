package agentloop

import (
	"context"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/core"
)

// protectAll returns a predicate that matches every non-empty path.
func protectAll(string) bool { return true }

func TestProtectPathsHook_SkipsWriteOnMatch(t *testing.T) {
	hook := ProtectPathsHook(protectAll)

	skip, err := hook.BeforeToolExecute(context.Background(), &core.ToolCall{
		ToolName: "write",
		Input:    `{"path":"x_test.go","content":"nope"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skip == nil {
		t.Fatal("expected Skip on protected path")
	}
	if !strings.Contains(skip.Error, "blocked:") {
		t.Errorf("Skip.Error = %q, want it to contain 'blocked:'", skip.Error)
	}
	if !strings.Contains(skip.Error, "x_test.go") {
		t.Errorf("Skip.Error = %q, want it to name the protected path", skip.Error)
	}
	if skip.Output != "" {
		t.Errorf("Skip.Output = %q, want empty (Error takes precedence)", skip.Output)
	}
}

func TestProtectPathsHook_AllowsWriteOnNoMatch(t *testing.T) {
	hook := ProtectPathsHook(func(string) bool { return false })

	skip, err := hook.BeforeToolExecute(context.Background(), &core.ToolCall{
		ToolName: "write",
		Input:    `{"path":"x.go","content":"package x"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skip != nil {
		t.Errorf("expected no Skip, got %+v", skip)
	}
}

func TestProtectPathsHook_OnlyAppliesToWriteAndEdit(t *testing.T) {
	// A predicate that matches everything must NOT block tools other
	// than write/edit — read/grep/shell etc. flow through normally.
	hook := ProtectPathsHook(protectAll)

	for _, name := range []string{"read", "grep", "find", "shell", "finish"} {
		t.Run(name, func(t *testing.T) {
			skip, err := hook.BeforeToolExecute(context.Background(), &core.ToolCall{
				ToolName: name,
				Input:    `{"path":"anything","content":"x"}`,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if skip != nil {
				t.Errorf("tool %q must not be blocked by path protection; got Skip=%+v", name, skip)
			}
		})
	}
}

func TestProtectPathsHook_NoOpAfterToolExecute(t *testing.T) {
	// AfterToolExecute is unused by ProtectPathsHook — call it and
	// verify it doesn't mutate the result.
	hook := ProtectPathsHook(protectAll)
	res := &core.ToolResult{ToolName: "write", Output: "unchanged"}
	hook.AfterToolExecute(context.Background(), core.ToolCall{ToolName: "write"}, res)
	if res.Output != "unchanged" {
		t.Errorf("AfterToolExecute mutated Output to %q, want unchanged", res.Output)
	}
}
