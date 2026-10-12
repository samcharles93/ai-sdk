package agentloop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
)

func TestRunHooks(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovery", true: "provider error"}[failed], func(t *testing.T) {
			p := &scriptProvider{steps: []chat.Response{
				toolStep("write", `{"path":"x","content":"hello"}`),
				toolStep("custom", `{}`),
				{Role: chat.RoleAssistant, Content: "done", FinishReason: "stop"},
				toolStep("finish", `{"status":"passed","summary":"done"}`),
			}}
			if failed {
				p.err = errors.New("provider unavailable")
			}
			var names []string
			started, finished := 0, 0
			_, err := Run(context.Background(), Config{
				Provider: p, Model: "m", WorkDir: t.TempDir(), Completion: CompletionForceFinish,
				Extra:     core.ToolSet{"custom": core.NewTool("custom", "", nil, func(context.Context, string) (string, error) { return "ok", nil })},
				ToolHooks: []core.ToolHook{core.ToolHookFuncs(nil, func(_ context.Context, call core.ToolCall, r *core.ToolResult) { names = append(names, call.ToolName) })},
				ModelHooks: []core.ModelHook{core.ModelHookFuncs(func(context.Context, chat.Request) { started++ }, func(_ context.Context, _ chat.Request, resp *chat.Response, err error, _ time.Duration, usage *chat.Usage) {
					finished++
					if failed && (err == nil || resp != nil || usage != nil) {
						t.Error("failure observer received successful response")
					}
					if !failed && (err != nil || resp == nil || usage == nil) {
						t.Error("success observer missing response")
					}
				})},
			})
			if (err != nil) != failed {
				t.Fatalf("err=%v", err)
			}
			if started != len(p.requests) || finished != started {
				t.Fatalf("started=%d finished=%d requests=%d", started, finished, len(p.requests))
			}
			if !failed && !reflect.DeepEqual(names, []string{"write", "custom", "finish"}) {
				t.Fatalf("observed tools=%v", names)
			}
		})
	}
}

func TestCallerHooksCannotBypassInternalGuards(t *testing.T) {
	for _, mode := range []string{"protected path", "finish", "gate parked", "loop break"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			step := toolStep("write", `{"path":"allowed","content":"x"}`)
			cfg := Config{ProtectPaths: func(p string) bool { return mode == "protected path" && p == "protected" }}
			switch mode {
			case "finish":
				stop := toolStep("finish", `{"status":"passed","summary":"done"}`)
				step.ToolCalls = append(stop.ToolCalls, step.ToolCalls...)
			case "gate parked":
				cfg.Gate = GateConfig{Commands: []GateCommand{{Name: "red", Argv: []string{"false"}}}, MaxConsecutiveFailures: 1}
				stop := toolStep("check", `{}`)
				step.ToolCalls = append(stop.ToolCalls, step.ToolCalls...)
			case "loop break":
				cfg.Extra = core.ToolSet{"repeat": core.NewTool("repeat", "", nil, func(context.Context, string) (string, error) { return "ok", nil })}
				for i := 0; i < 6; i++ {
					step.ToolCalls = append(toolStep("repeat", `{}`).ToolCalls, step.ToolCalls...)
				}
			}
			denied := false
			cfg.ToolHooks = []core.ToolHook{core.ToolHookFuncs(func(_ context.Context, c *core.ToolCall) (*core.Skip, error) {
				if c.ToolName == "write" {
					c.Input = `{"path":"protected","content":"x"}`
				}
				return nil, nil
			}, func(_ context.Context, c core.ToolCall, r *core.ToolResult) {
				if c.ToolName == "write" {
					denied = r.Error != ""
				}
			})}
			runScript(t, dir, cfg, step)
			if !denied {
				t.Fatal("internal guard did not deny rewritten call")
			}
			if _, err := os.Stat(filepath.Join(dir, "protected")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("protected file exists: %v", err)
			}
		})
	}
}
