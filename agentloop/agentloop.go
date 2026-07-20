// Package agentloop runs a single autonomous agent mission to completion:
// one model, a jailed toolset, a quality gate that must pass before the
// run can succeed, budgets that bound it, and a structured result that
// reports what happened. It is the building block for headless agent
// pipelines (eval_loop, archie workflows); orchestration across missions
// lives with the caller.
package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
	"github.com/samcharles93/ai-sdk/runtime"
	"github.com/samcharles93/ai-sdk/toolkit"
)

// Status is the overall outcome of a run.
type Status string

const (
	// StatusPassed means the mission finished and the gate passed.
	StatusPassed Status = "passed"
	// StatusParked means the run stopped without a clean finish: gate
	// failures hit the cap, a budget ran out, the loop breaker fired, or
	// the model reported itself blocked. Parked runs carry the reason.
	StatusParked Status = "parked"
	// StatusIdle means the model ended the conversation without calling
	// finish and without changing anything.
	StatusIdle Status = "idle"
)

// Stop reasons reported in Result.StopReason.
const (
	StopFinished        = "finished"
	StopBudgetExhausted = "budget_exhausted"
	StopTimedOut        = "timed_out"
	StopLoopBreak       = "loop_break"
	StopGateParked      = "gate_parked"
	StopGateFailedFinal = "gate_failed_final"
	StopModelBlocked    = "model_blocked"
	StopIdle            = "idle"
	// StopEndedWithoutFinish: the model stopped producing tool calls
	// after making changes but never called finish — the mission state
	// is unknown, so the run parks for review.
	StopEndedWithoutFinish = "ended_without_finish"
)

// Result is the structured outcome of a run. It is the caller's PR body,
// state-machine transition, and notes write-back in one place.
type Result struct {
	Status     Status   `json:"status"`
	StopReason string   `json:"stop_reason"`
	Changes    []string `json:"changes,omitempty"` // files written/edited, in first-touch order
	Iterations int      `json:"iterations"`
	TokensUsed int      `json:"tokens_used"`
	Summary    string   `json:"summary,omitempty"` // from the finish tool
	Detail     string   `json:"detail,omitempty"`  // last gate failure / park explanation
}

// Budget bounds a run. Zero values disable the corresponding limit.
type Budget struct {
	MaxSteps  int
	MaxTokens int
	WallClock time.Duration
}

// NotesStore is caller-side persistent memory across runs. Entries follow
// the eval_loop convention: every note must cite how it was verified
// (verified_by), and stores live outside the work tree so they neither
// pollute diffs nor vanish with the worktree.
type NotesStore interface {
	Load(ctx context.Context) (string, error)
	Append(ctx context.Context, entry string) error
}

// Config configures a single run.
type Config struct {
	// Runtime + ModelRef select the model ("provider/model"). Provider,
	// when non-nil, bypasses the runtime (tests, custom providers) and
	// Model names the model to request from it.
	Runtime  *runtime.Runtime
	ModelRef string
	Provider chat.Provider
	Model    string

	// WorkDir is the directory the toolset is jailed to and gate
	// commands run in.
	WorkDir string

	// SystemTmpl is a text/template rendered with PromptData. Empty
	// selects DefaultSystemTemplate.
	SystemTmpl string
	// Mission is the task statement, injected into the first user message.
	Mission string
	// ExtraRules is free-form guidance appended to the rendered prompt.
	ExtraRules string

	// PreloadFiles are read from WorkDir and included in the initial
	// context (the eval_loop "preload everything" strategy). Missing
	// files are skipped with a log line.
	PreloadFiles []string

	// Notes, when non-nil, is loaded into the initial context and
	// exposed to the model via a write_note tool.
	Notes NotesStore

	// Gate is the quality gate. An empty command list disables gating.
	Gate GateConfig
	// Preflight commands run before the loop; their output is injected
	// as ground truth (toolchain versions, go fix -diff, ...).
	Preflight []GateCommand

	Budget Budget

	// ReadOnly registers only read/grep/find (planner/analysis missions).
	ReadOnly bool
	// ProtectPaths, when non-nil, blocks write/edit on matching paths
	// (relative, as the model supplies them) with an in-band refusal —
	// an environmental constraint where a prompt rule would be advisory
	// (e.g. a TDD fix stage protecting the committed repro tests).
	ProtectPaths func(path string) bool
	// Extra tools are merged into the toolset after the built-ins.
	Extra core.ToolSet

	Logger *slog.Logger
}

// Run executes the mission to completion and reports what happened. The
// returned error is reserved for infrastructure failures (provider
// resolution, provider errors); mission-level failures are expressed in
// Result.Status.
func Run(ctx context.Context, cfg Config) (Result, error) {
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	provider, model, err := resolveProvider(ctx, cfg)
	if err != nil {
		return Result{}, err
	}

	if cfg.Budget.WallClock > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Budget.WallClock)
		defer cancel()
	}

	state := &runState{}

	tools, err := buildToolSet(cfg, state, log)
	if err != nil {
		return Result{}, err
	}

	system, first, err := buildMessages(ctx, cfg)
	if err != nil {
		return Result{}, err
	}

	maxSteps := cfg.Budget.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 40
	}

	res, genErr := core.GenerateText(ctx, provider, core.GenerateOptions{
		Model:    model,
		System:   system,
		Messages: []chat.Message{{Role: chat.RoleUser, Content: first}},
		Tools:    tools,
		MaxSteps: maxSteps,
		StopWhen: core.AnyCondition(
			core.StepCountIs(maxSteps),
			TokenBudgetIs(cfg.Budget.MaxTokens),
			state.stopRequested,
		),
	})

	result := classify(ctx, cfg, state, res, genErr, maxSteps)
	log.Info(
		"agentloop run finished",
		"status", result.Status,
		"stop_reason", result.StopReason,
		"iterations", result.Iterations,
		"tokens", result.TokensUsed,
		"changes", len(result.Changes),
	)
	if genErr != nil && !errors.Is(genErr, context.DeadlineExceeded) && !errors.Is(genErr, context.Canceled) {
		return result, genErr
	}
	return result, nil
}

// runState is the shared mutable state that tool wrappers write and stop
// conditions read. All access happens from the generation loop's tool
// execution, which core runs sequentially, plus post-run classification —
// no locking needed.
type runState struct {
	gate      gateState
	loop      loopState
	changes   []string
	changeSet map[string]bool

	finishCalled  bool
	finishStatus  string
	finishSummary string
}

// recordChange remembers a mutated path once, in first-touch order.
func (s *runState) recordChange(path string) {
	if s.changeSet == nil {
		s.changeSet = make(map[string]bool)
	}
	if !s.changeSet[path] {
		s.changeSet[path] = true
		s.changes = append(s.changes, path)
	}
}

// stopRequested is a core.StopCondition that halts the loop when a tool
// wrapper has flagged a terminal condition (gate parked, loop breaker
// hard stop, finish called).
func (s *runState) stopRequested([]core.StepResult) bool {
	return s.gate.parked || s.loop.hardStop || s.finishCalled
}

// TokenBudgetIs returns a stop condition that halts once cumulative token
// usage across steps reaches maxTokens. Zero or negative disables it.
// Like all stop conditions it is evaluated between steps, so a run can
// overshoot by at most one model call.
func TokenBudgetIs(maxTokens int) core.StopCondition {
	return func(steps []core.StepResult) bool {
		if maxTokens <= 0 {
			return false
		}
		total := 0
		for _, s := range steps {
			total += s.Usage.TotalTokens
		}
		return total >= maxTokens
	}
}

func resolveProvider(ctx context.Context, cfg Config) (chat.Provider, string, error) {
	if cfg.Provider != nil {
		return cfg.Provider, cfg.Model, nil
	}
	if cfg.Runtime == nil || cfg.ModelRef == "" {
		return nil, "", errors.New("agentloop: either Provider+Model or Runtime+ModelRef must be set")
	}
	return cfg.Runtime.ChatProvider(ctx, cfg.ModelRef)
}

// buildToolSet assembles built-ins (jailed to WorkDir), wraps them with
// the gate and the loop breaker, and adds the finish/write_note tools.
func buildToolSet(cfg Config, state *runState, log *slog.Logger) (core.ToolSet, error) {
	reg := toolkit.NewRegistry()
	if err := toolkit.RegisterBuiltins(reg, cfg.WorkDir); err != nil {
		return nil, err
	}

	set := reg.CoreToolSet(toolkit.HeadlessBridge{Logger: log})
	if cfg.ReadOnly {
		for name := range set {
			if !readOnlyTools[name] {
				delete(set, name)
			}
		}
	}

	set = protectedToolSet(set, cfg.ProtectPaths)
	set = changeTrackedToolSet(set, state)
	set = gatedToolSet(set, &state.gate, cfg.Gate, cfg.WorkDir, log)
	set = loopBrokenToolSet(set, &state.loop)

	for name, tool := range cfg.Extra {
		set[name] = tool
	}

	set["finish"] = finishTool(state)
	if cfg.Notes != nil {
		set["write_note"] = writeNoteTool(cfg.Notes)
	}
	return set, nil
}

// readOnlyTools are the built-ins available to ReadOnly (planner) runs.
var readOnlyTools = map[string]bool{"read": true, "grep": true, "find": true}

// mutatingTools are the built-ins whose success can change files and
// therefore trigger the gate. Shell is included: any command may mutate.
var mutatingTools = map[string]bool{"write": true, "edit": true, "shell": true}

// finishTool lets the model end the run deliberately and supply the
// summary that becomes the PR body / notes write-back.
func finishTool(state *runState) *core.Tool {
	params := json.RawMessage(`{
		"type": "object",
		"properties": {
			"status": {"type": "string", "enum": ["passed", "blocked"], "description": "passed: mission accomplished. blocked: cannot make further progress; explain why in summary."},
			"summary": {"type": "string", "description": "What was done, what was verified, and anything the reviewer must know. This text is shown to a human."}
		},
		"required": ["status", "summary"]
	}`)
	return core.NewTool("finish",
		"End the run. Call this exactly once, when the mission is complete (status=passed) or genuinely stuck (status=blocked).",
		params,
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Status  string `json:"status"`
				Summary string `json:"summary"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "finish rejected: invalid arguments: " + err.Error(), nil
			}
			if args.Summary == "" {
				return "finish rejected: summary is required", nil
			}
			state.finishCalled = true
			state.finishStatus = args.Status
			state.finishSummary = args.Summary
			return "run ending with status " + args.Status, nil
		})
}

// writeNoteTool appends a verified observation to the caller's NotesStore.
func writeNoteTool(notes NotesStore) *core.Tool {
	params := json.RawMessage(`{
		"type": "object",
		"properties": {
			"note": {"type": "string", "description": "The observation worth remembering across runs."},
			"verified_by": {"type": "string", "description": "The exact command or evidence that proves this note (e.g. 'go doc x/y.Z', 'go test -run TestFoo'). Required: unverified notes are misinformation."}
		},
		"required": ["note", "verified_by"]
	}`)
	return core.NewTool("write_note",
		"Persist a verified observation for future runs on this project. Only write facts you have proven this run; cite the proof in verified_by.",
		params,
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Note       string `json:"note"`
				VerifiedBy string `json:"verified_by"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "note rejected: invalid arguments: " + err.Error(), nil
			}
			if args.Note == "" || args.VerifiedBy == "" {
				return "note rejected: note and verified_by are both required", nil
			}
			if err := notes.Append(ctx, args.Note+" (verified_by: "+args.VerifiedBy+")"); err != nil {
				return "note rejected: " + err.Error(), nil
			}
			return "note saved", nil
		})
}

// classify turns the raw generation outcome plus run state into a Result.
func classify(ctx context.Context, cfg Config, state *runState, res core.GenerateResult, genErr error, maxSteps int) Result {
	r := Result{
		Changes:    state.changes,
		Iterations: len(res.Steps),
		TokensUsed: res.TotalUsage.TotalTokens,
		Summary:    state.finishSummary,
	}

	switch {
	case genErr != nil && (errors.Is(genErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded)):
		r.Status, r.StopReason = StatusParked, StopTimedOut
	case genErr != nil && errors.Is(genErr, context.Canceled):
		r.Status, r.StopReason = StatusParked, StopTimedOut
	case genErr != nil:
		r.Status, r.StopReason = StatusParked, "provider_error"
		r.Detail = genErr.Error()
	case state.gate.parked:
		r.Status, r.StopReason = StatusParked, StopGateParked
		r.Detail = state.gate.lastFailure
	case state.loop.hardStop:
		r.Status, r.StopReason = StatusParked, StopLoopBreak
		r.Detail = state.loop.message
	case state.finishCalled && state.finishStatus == "blocked":
		r.Status, r.StopReason = StatusParked, StopModelBlocked
	case state.finishCalled:
		// The model claims success; the gate is the authority. Re-run it
		// once if anything was mutated.
		if len(state.changes) > 0 && len(cfg.Gate.Commands) > 0 {
			if out, err := runGate(ctx, cfg.Gate.Commands, cfg.WorkDir); err != nil {
				r.Status, r.StopReason = StatusParked, StopGateFailedFinal
				r.Detail = out
				return r
			}
		}
		r.Status, r.StopReason = StatusPassed, StopFinished
	case len(state.changes) == 0:
		r.Status, r.StopReason = StatusIdle, StopIdle
		r.Detail = lastText(res)
	case len(res.Steps) >= maxSteps ||
		(cfg.Budget.MaxTokens > 0 && res.TotalUsage.TotalTokens >= cfg.Budget.MaxTokens):
		r.Status, r.StopReason = StatusParked, StopBudgetExhausted
		r.Detail = fmt.Sprintf("stopped after %d/%d steps, %d tokens", len(res.Steps), maxSteps, res.TotalUsage.TotalTokens)
	default:
		// The model went quiet mid-mission without calling finish.
		r.Status, r.StopReason = StatusParked, StopEndedWithoutFinish
		r.Detail = lastText(res)
	}
	return r
}

func lastText(res core.GenerateResult) string {
	if res.Text != "" {
		return res.Text
	}
	for i := len(res.Steps) - 1; i >= 0; i-- {
		if res.Steps[i].Text != "" {
			return res.Steps[i].Text
		}
	}
	return ""
}

// FileNotesStore is a NotesStore backed by a single file (eval_loop's
// AGENT_NOTES.md pattern).
type FileNotesStore struct{ Path string }

func (f FileNotesStore) Load(context.Context) (string, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

func (f FileNotesStore) Append(_ context.Context, entry string) error {
	fh, err := os.OpenFile(f.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer fh.Close()
	_, err = fh.WriteString("- " + entry + "\n")
	return err
}
