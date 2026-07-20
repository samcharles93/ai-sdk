package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/samcharles93/ai-sdk/core"
)

// GateCommand is one command in the quality gate, run in the work
// directory. ExpectFailure inverts success: the command must exit
// nonzero (a TDD repro stage requires the new tests to FAIL before the
// fix is written).
type GateCommand struct {
	Name          string
	Argv          []string
	ExpectFailure bool
}

// GateConfig is the quality gate for a run: the command list that must
// pass after mutations, and how many consecutive failing cycles are
// tolerated before the run parks instead of flailing.
type GateConfig struct {
	Commands []GateCommand
	// MaxConsecutiveFailures parks the run after N failing gate cycles
	// in a row. Zero means the default of 5.
	MaxConsecutiveFailures int
}

func (g GateConfig) maxFailures() int {
	if g.MaxConsecutiveFailures > 0 {
		return g.MaxConsecutiveFailures
	}
	return 5
}

// gateState tracks the dirty/parked status of the gate across tool calls.
type gateState struct {
	dirty            bool
	consecutiveFails int
	parked           bool
	lastFailure      string
}

// gateOutputLimit bounds how much failing command output is fed back to
// the model per gate cycle.
const gateOutputLimit = 4096

// gatedToolSet wraps the toolset with the quality gate:
//
//   - write/edit record the touched path and trigger a gate cycle; the
//     cycle's outcome is appended to the tool result so the model sees
//     breakage immediately.
//   - while the gate is failing, find and grep are blocked ("fix the
//     gate first") to stop the model wandering off; read stays available
//     because fixing requires it, and shell stays available as the
//     escape hatch for investigation.
//   - MaxConsecutiveFailures failing cycles in a row set parked, which
//     the run's stop condition observes.
//
// An empty command list disables all of this.
func gatedToolSet(set core.ToolSet, g *gateState, cfg GateConfig, workdir string, log *slog.Logger) core.ToolSet {
	if len(cfg.Commands) == 0 {
		return set
	}
	out := make(core.ToolSet, len(set))
	for name, tool := range set {
		switch {
		case name == "write" || name == "edit":
			out[name] = wrapMutating(tool, g, cfg, workdir, log)
		case name == "find" || name == "grep":
			out[name] = wrapBlockedWhileDirty(tool, g)
		default:
			out[name] = tool
		}
	}
	return out
}

// protectedToolSet refuses write/edit on protected paths before the
// tool ever runs — the refusal is fed back to the model in-band.
func protectedToolSet(set core.ToolSet, protect func(string) bool) core.ToolSet {
	if protect == nil {
		return set
	}
	out := make(core.ToolSet, len(set))
	for name, tool := range set {
		if name != "write" && name != "edit" {
			out[name] = tool
			continue
		}
		inner := tool.Execute
		wrapped := *tool
		wrapped.Execute = func(ctx context.Context, input string) (string, error) {
			if path := pathFromArgs(input); path != "" && protect(path) {
				return "blocked: " + path + " is protected in this stage and must not be modified. It is part of the mission's specification — change the code it exercises instead.", nil
			}
			return inner(ctx, input)
		}
		out[name] = &wrapped
	}
	return out
}

// changeTrackedToolSet records the touched path of every successful
// write/edit, independently of whether a gate is configured.
func changeTrackedToolSet(set core.ToolSet, state *runState) core.ToolSet {
	out := make(core.ToolSet, len(set))
	for name, tool := range set {
		if name != "write" && name != "edit" {
			out[name] = tool
			continue
		}
		inner := tool.Execute
		wrapped := *tool
		wrapped.Execute = func(ctx context.Context, input string) (string, error) {
			res, err := inner(ctx, input)
			if err == nil {
				if path := pathFromArgs(input); path != "" {
					state.recordChange(path)
				}
			}
			return res, err
		}
		out[name] = &wrapped
	}
	return out
}

func wrapMutating(tool *core.Tool, g *gateState, cfg GateConfig, workdir string, log *slog.Logger) *core.Tool {
	inner := tool.Execute
	wrapped := *tool
	wrapped.Execute = func(ctx context.Context, input string) (string, error) {
		res, err := inner(ctx, input)
		if err != nil {
			return res, err
		}

		gateOut, gateErr := runGate(ctx, cfg.Commands, workdir)
		if gateErr == nil {
			g.dirty = false
			g.consecutiveFails = 0
			return res + "\n\n[gate] all gate commands passed", nil
		}
		g.dirty = true
		g.consecutiveFails++
		g.lastFailure = gateOut
		log.Warn("gate cycle failed", "consecutive", g.consecutiveFails, "max", cfg.maxFailures())
		if g.consecutiveFails >= cfg.maxFailures() {
			g.parked = true
			return res + fmt.Sprintf("\n\n[gate] FAILED (%d consecutive failing cycles — parking the run):\n%s",
				g.consecutiveFails, gateOut), nil
		}
		return res + fmt.Sprintf("\n\n[gate] FAILED (cycle %d of %d before the run parks) — fix this before anything else:\n%s",
			g.consecutiveFails, cfg.maxFailures(), gateOut), nil
	}
	return &wrapped
}

func wrapBlockedWhileDirty(tool *core.Tool, g *gateState) *core.Tool {
	inner := tool.Execute
	wrapped := *tool
	wrapped.Execute = func(ctx context.Context, input string) (string, error) {
		if g.dirty {
			return "blocked: the quality gate is currently failing. Fix the gate before exploring further. Last failure:\n" + g.lastFailure, nil
		}
		return inner(ctx, input)
	}
	return &wrapped
}

// pathFromArgs extracts the "path" argument shared by the write and edit
// tool schemas. Empty on parse failure — recording changes is best-effort;
// the caller's git diff is the ground truth.
func pathFromArgs(input string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return ""
	}
	return args.Path
}

// runGate executes the gate commands in order and returns the first
// violation's output as the error text. All commands passing returns
// ("", nil).
func runGate(ctx context.Context, cmds []GateCommand, workdir string) (string, error) {
	for _, gc := range cmds {
		if len(gc.Argv) == 0 {
			continue
		}
		cmd := exec.CommandContext(ctx, gc.Argv[0], gc.Argv[1:]...)
		cmd.Dir = workdir
		out, err := cmd.CombinedOutput()
		switch {
		case gc.ExpectFailure && err == nil:
			msg := fmt.Sprintf("[%s] expected this command to FAIL (e.g. reproducing tests for an unfixed bug), but it passed:\n%s",
				gc.Name, truncateOutput(out))
			return msg, fmt.Errorf("gate %s: expected failure but command passed", gc.Name)
		case !gc.ExpectFailure && err != nil:
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			msg := fmt.Sprintf("[%s] %s\n%s", gc.Name, strings.Join(gc.Argv, " "), truncateOutput(out))
			return msg, fmt.Errorf("gate %s: %w", gc.Name, err)
		}
	}
	return "", nil
}

func truncateOutput(out []byte) string {
	s := strings.TrimSpace(string(out))
	if len(s) <= gateOutputLimit {
		return s
	}
	return s[:gateOutputLimit/2] + "\n...[truncated]...\n" + s[len(s)-gateOutputLimit/2:]
}
