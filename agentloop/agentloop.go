// Package agentloop runs a single autonomous agent mission to completion:
// one model, a toolset whose file tools are confined to the work directory, a
// quality gate that must pass before the run can succeed, budgets that bound
// it, and a structured result that reports what happened. It is the building
// block for headless agent pipelines (eval_loop, archie workflows);
// orchestration across missions lives with the caller.
package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
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
	// StopCanceled: the caller cancelled the run. Unlike a wall-clock
	// timeout it must not be retried automatically.
	StopCanceled        = "canceled"
	StopProviderError   = "provider_error"
	StopContextLimit    = "context_limit"
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
	Status     Status     `json:"status"`
	StopReason string     `json:"stop_reason"`
	Changes    []string   `json:"changes,omitempty"` // files written/edited, in first-touch order
	Iterations int        `json:"iterations"`
	TokensUsed int        `json:"tokens_used"`
	Usage      chat.Usage `json:"usage"`
	Summary    string     `json:"summary,omitempty"` // from the finish tool
	Detail     string     `json:"detail,omitempty"`  // last gate failure / park explanation
}

// Budget bounds a run. Zero values disable the corresponding limit.
type Budget struct {
	MaxSteps  int
	MaxTokens int
	WallClock time.Duration
}

// CompletionMode controls the bounded recovery attempted when a model ends a
// run without calling its required finish tool.
type CompletionMode string

const (
	// CompletionNone leaves an unfinished model response classified as-is.
	CompletionNone CompletionMode = ""
	// CompletionForceFinish makes one final, single-step call that requires the
	// finish tool. A text response from that call is still unfinished and is
	// never accepted as completion.
	CompletionForceFinish CompletionMode = "force_finish"
)

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
	// Model names the model to request from it. ModelInfo carries the
	// request-shaping flags for the explicit Provider path; the runtime
	// path resolves them from catalog metadata instead.
	Runtime   *runtime.Runtime
	ModelRef  string
	Provider  chat.Provider
	Model     string
	ModelInfo chat.ModelInfo

	// WorkDir is the directory the file tools are confined to and gate
	// commands run in. The shell tool starts there but is not confined to
	// it, and Extra tools are not confined at all.
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

	// Completion controls recovery of a normally stopped run that omitted the
	// required finish tool. It never retries errors, budgets, or hard stops.
	Completion CompletionMode

	// Compact enables automatic history compaction. When enabled, a run
	// whose history approaches the model's context window summarises older
	// turns into a continuation summary instead of exhausting the budget.
	Compact CompactionConfig

	// ReadOnly registers only read/grep/find (planner/analysis missions).
	ReadOnly bool
	// ProtectPaths, when non-nil, blocks write/edit on matching paths
	// with an in-band refusal — an environmental constraint where a prompt
	// rule would be advisory (e.g. a TDD fix stage protecting the committed
	// repro tests). The predicate receives the canonical WorkDir-relative
	// path, so equivalent spellings of one file reach the same decision; a
	// path escaping WorkDir arrives absolute. It is a policy hook, not a
	// sandbox: shell and Extra tools are not covered.
	ProtectPaths func(path string) bool
	// Extra tools are merged into the toolset after the built-ins and
	// wrapped with them; an Extra named write or edit replaces the built-in
	// but is still tracked. An Extra runs exclusively unless it declares
	// its own Concurrency.
	Extra core.ToolSet
	// MaxParallelToolCalls bounds how many of one step's tool calls run at
	// once. One or less runs them in order.
	MaxParallelToolCalls int

	// ToolHooks intercept all tools, including built-ins and completion recovery.
	// Internal guards validate the resulting call after caller Before hooks.
	ToolHooks []core.ToolHook
	// ModelHooks observe every model call, including compaction and recovery.
	ModelHooks []core.ModelHook

	Logger *slog.Logger
}

// Run executes the mission to completion and reports what happened. The
// returned error reports provider/configuration failures, including context
// that cannot fit the configured window; mission-level failures are expressed in
// Result.Status.
func Run(ctx context.Context, cfg Config) (Result, error) {
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	provider, model, modelInfo, err := resolveProvider(ctx, cfg)
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

	recoveryMessages := baseMessages(system, first)
	compact := compactorFor(cfg, provider, model, modelInfo, tools, log)
	onStep := func(ctx context.Context, messages []chat.Message) ([]chat.Message, error) {
		next := messages
		if compact != nil {
			var err error
			next, err = compact.onStep(ctx, messages)
			if err != nil {
				return nil, err
			}
			if next == nil {
				next = messages
			}
		}
		recoveryMessages = append(recoveryMessages[:0], next...)
		return next, nil
	}

	initialMessages, err := onStep(ctx, baseMessages(system, first))
	if err != nil {
		return classify(ctx, cfg, state, core.GenerateResult{}, err, maxSteps), err
	}

	toolHooks := append([]core.ToolHook(nil), cfg.ToolHooks...)
	toolHooks = append(toolHooks, core.ToolHookFuncs(func(_ context.Context, _ *core.ToolCall) (*core.Skip, error) {
		if state.stopRequested(nil) {
			return &core.Skip{Error: "blocked: run has already stopped"}, nil
		}
		return nil, nil
	}, nil))
	if cfg.ProtectPaths != nil {
		toolHooks = append(toolHooks, ProtectPathsHook(cfg.WorkDir, cfg.ProtectPaths))
	}

	res, genErr := core.GenerateText(ctx, provider, core.GenerateOptions{
		Model:     model,
		ModelInfo: modelInfo,
		Messages:  initialMessages,
		Tools:     tools,
		MaxSteps:  maxSteps,

		MaxParallelToolCalls: cfg.MaxParallelToolCalls,
		StopWhen: core.AnyCondition(
			core.StepCountIs(maxSteps),
			budgetIs(cfg.Budget.MaxTokens, compact),
			state.stopRequested,
		),
		OnStep:     onStep,
		ToolHooks:  toolHooks,
		ModelHooks: cfg.ModelHooks,
	})
	var compacted chat.Usage
	if compact != nil {
		compacted = compact.usage
		res.TotalUsage = addUsage(res.TotalUsage, compacted)
	}
	settleTotal(&res, compacted)
	if shouldForceFinish(cfg, state, res, genErr, maxSteps) {
		recovery, err := forceFinish(ctx, provider, model, modelInfo, system, first, recoveryMessages, res, tools, toolHooks, cfg.ModelHooks, compact)
		res = combineResults(res, recovery)
		settleTotal(&res, compacted)
		if err != nil {
			genErr = err
		}
	}
	for i, step := range res.Steps {
		log.Info(
			"agentloop step usage",
			"iteration", i+1,
			"prompt_tokens", step.Usage.PromptTokens,
			"completion_tokens", step.Usage.CompletionTokens,
			"cached_tokens", step.Usage.CachedTokens,
			"cache_creation_tokens", step.Usage.CacheCreationTokens,
			"total_tokens", step.Usage.TotalTokens,
		)
	}

	result := classify(ctx, cfg, state, res, genErr, maxSteps)
	log.Info(
		"agentloop run finished",
		"status", result.Status,
		"stop_reason", result.StopReason,
		"iterations", result.Iterations,
		"tokens", result.TokensUsed,
		"changes", len(result.Changes),
	)
	if genErr != nil && ctx.Err() == nil {
		return result, genErr
	}
	return result, nil
}

func baseMessages(system, first string) []chat.Message {
	messages := make([]chat.Message, 0, 2)
	if system != "" {
		messages = append(messages, chat.Message{Role: chat.RoleSystem, Content: system})
	}
	return append(messages, chat.Message{Role: chat.RoleUser, Content: first})
}

func shouldForceFinish(cfg Config, state *runState, res core.GenerateResult, genErr error, maxSteps int) bool {
	return cfg.Completion == CompletionForceFinish &&
		genErr == nil &&
		!state.stopRequested(nil) &&
		res.FinishReason == core.FinishReasonStop &&
		len(res.Steps) < maxSteps &&
		(cfg.Budget.MaxTokens <= 0 || res.TotalUsage.TotalTokens < cfg.Budget.MaxTokens)
}

func forceFinish(ctx context.Context, provider chat.Provider, model string, info chat.ModelInfo, system, first string, messages []chat.Message, previous core.GenerateResult, tools core.ToolSet, toolHooks []core.ToolHook, modelHooks []core.ModelHook, compact *compactor) (core.GenerateResult, error) {
	if len(messages) == 0 {
		messages = baseMessages(system, first)
	}
	if len(previous.Steps) > 0 {
		last := previous.Steps[len(previous.Steps)-1]
		messages = append(messages, chat.Message{Role: chat.RoleAssistant, Content: last.Text, Parts: last.Parts})
	}
	messages = append(messages, chat.Message{
		Role:    chat.RoleUser,
		Content: "Your previous response ended without completing this run. Call the finish tool now with the correct status and a concise, verified summary. Do not reply with text instead of calling finish.",
	})
	if err := compact.checkSize(messages); err != nil {
		return core.GenerateResult{}, err
	}
	return core.GenerateText(ctx, provider, core.GenerateOptions{
		Model:      model,
		ModelInfo:  info,
		Messages:   messages,
		Tools:      tools,
		ToolHooks:  toolHooks,
		ModelHooks: modelHooks,
		MaxSteps:   1,
		ToolChoice: &chat.ToolChoice{Type: chat.ToolChoiceTool, Name: "finish"},
	})
}

func combineResults(first, second core.GenerateResult) core.GenerateResult {
	if len(second.Steps) == 0 {
		return first
	}
	first.Steps = append(first.Steps, second.Steps...)
	first.ToolCalls = append(first.ToolCalls, second.ToolCalls...)
	first.ToolResults = append(first.ToolResults, second.ToolResults...)
	first.Warnings = append(first.Warnings, second.Warnings...)
	first.TotalUsage = addUsage(first.TotalUsage, second.TotalUsage)
	first.FinishReason = second.FinishReason
	first.Text = second.Text
	first.Parts = second.Parts
	first.Reasoning = second.Reasoning
	return first
}

func addUsage(a, b chat.Usage) chat.Usage {
	return chat.Usage{
		PromptTokens:        a.PromptTokens + b.PromptTokens,
		CompletionTokens:    a.CompletionTokens + b.CompletionTokens,
		TotalTokens:         a.TotalTokens + b.TotalTokens,
		CachedTokens:        a.CachedTokens + b.CachedTokens,
		CacheCreationTokens: a.CacheCreationTokens + b.CacheCreationTokens,
	}
}

// runState is the shared mutable state that tool wrappers write and stop
// conditions read. Stop conditions and classification run between steps,
// after every call has returned; within a step, finish and check run
// exclusively, so only change tracking and the loop breaker see concurrent
// calls and need locks.
type runState struct {
	gate gateState
	loop loopState

	mu        sync.Mutex
	changes   []string
	changeSet map[string]bool

	finishCalled  bool
	finishStatus  string
	finishSummary string
}

// recordChange remembers a mutated path once, in first-touch order.
func (s *runState) recordChange(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return budgetIs(maxTokens, nil)
}

// budgetIs is TokenBudgetIs for a run, counting compaction calls too when
// compact is set, so the stop condition and the reported total agree.
func budgetIs(maxTokens int, compact *compactor) core.StopCondition {
	return func(steps []core.StepResult) bool {
		if maxTokens <= 0 {
			return false
		}
		var compacted chat.Usage
		if compact != nil {
			compacted = compact.usage
		}
		return spentTokens(steps, compacted) >= maxTokens
	}
}

// effectiveTotal is the tokens a usage record is charged for. Providers that
// omit total_tokens report 0, so fall back to prompt plus completion.
func effectiveTotal(u chat.Usage) int {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.PromptTokens + u.CompletionTokens
}

// spentTokens is the run's token total: every step plus compaction calls,
// which are billed like any other model call.
func spentTokens(steps []core.StepResult, compacted chat.Usage) int {
	total := effectiveTotal(compacted)
	for _, s := range steps {
		total += effectiveTotal(s.Usage)
	}
	return total
}

// settleTotal writes the run's spent total into res.TotalUsage so the stop
// condition, force-finish check and classification all read the same number.
func settleTotal(res *core.GenerateResult, compacted chat.Usage) {
	res.TotalUsage.TotalTokens = spentTokens(res.Steps, compacted)
}

func resolveProvider(ctx context.Context, cfg Config) (chat.Provider, string, chat.ModelInfo, error) {
	if cfg.Provider != nil {
		return cfg.Provider, cfg.Model, cfg.ModelInfo, nil
	}
	if cfg.Runtime == nil || cfg.ModelRef == "" {
		return nil, "", chat.ModelInfo{}, errors.New("agentloop: either Provider+Model or Runtime+ModelRef must be set")
	}
	provider, model, err := cfg.Runtime.ChatProvider(ctx, cfg.ModelRef)
	if err != nil {
		return nil, "", chat.ModelInfo{}, err
	}
	info, err := cfg.Runtime.ChatModelInfo(cfg.ModelRef)
	if err != nil {
		return nil, "", chat.ModelInfo{}, err
	}
	return provider, model, info, nil
}

// buildToolSet assembles built-ins (file tools confined to WorkDir), wraps them
// with change tracking and the loop breaker, and adds the finish/write_note
// tools.
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

	// Extra tools join before wrapping so replacements are tracked too.
	declareConcurrency(set, cfg.WorkDir)
	for name, tool := range cfg.Extra {
		set[name] = tool
	}
	set["finish"] = finishTool(state)
	if len(cfg.Gate.Commands) > 0 {
		set["check"] = checkTool(&state.gate, cfg.Gate, cfg.WorkDir, log)
	}
	if cfg.Notes != nil {
		set["write_note"] = writeNoteTool(cfg.Notes)
	}

	// Wrap order matters: change tracking wraps the tool's success
	// path; the loop breaker sees everything (including blocked calls).
	// Path protection is a [core.ToolHook] instead; see ProtectPathsHook.
	set = changeTrackedToolSet(set, state)
	return loopBrokenToolSet(set, &state.loop), nil
}

// declareConcurrency marks which built-ins may overlap in a parallel step:
// searches run alongside anything, and file reads and writes are ordered per
// path. Shell and every other built-in stay exclusive.
func declareConcurrency(set core.ToolSet, workdir string) {
	byPath := func(input string) core.Concurrency {
		path := pathFromArgs(input)
		if path == "" {
			return core.Concurrency{}
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(workdir, path)
		}
		return core.Concurrency{Parallel: true, Key: filepath.Clean(path)}
	}
	for name, tool := range set {
		declared := *tool
		switch name {
		case "grep", "find":
			declared.Concurrency = core.RunParallel
		case "read", "write", "edit":
			declared.Concurrency = byPath
		default:
			continue
		}
		set[name] = &declared
	}
}

// readOnlyTools are the built-ins available to ReadOnly (planner) runs.
var readOnlyTools = map[string]bool{"read": true, "grep": true, "find": true}

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
				//nolint:nilerr // Malformed arguments are reported back to the model in-band so it can retry; a Go error would abort the run.
				return "finish rejected: invalid arguments: " + err.Error(), nil
			}
			if args.Status != "passed" && args.Status != "blocked" {
				return `finish rejected: status must be "passed" or "blocked"`, nil
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
				//nolint:nilerr // Malformed arguments are reported back to the model in-band so it can retry; a Go error would abort the run.
				return "note rejected: invalid arguments: " + err.Error(), nil
			}
			if args.Note == "" || args.VerifiedBy == "" {
				return "note rejected: note and verified_by are both required", nil
			}
			if err := notes.Append(ctx, args.Note+" (verified_by: "+args.VerifiedBy+")"); err != nil {
				//nolint:nilerr // A failed note write is reported to the model in-band; it must not abort the run.
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
		Usage:      res.TotalUsage,
		Summary:    state.finishSummary,
	}

	switch {
	case genErr != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		r.Status, r.StopReason = StatusParked, StopTimedOut
	case genErr != nil && errors.Is(ctx.Err(), context.Canceled):
		r.Status, r.StopReason = StatusParked, StopCanceled
	case errors.Is(genErr, ErrContextLimit):
		r.Status, r.StopReason = StatusParked, StopContextLimit
		r.Detail = genErr.Error()
	case genErr != nil:
		r.Status, r.StopReason = StatusParked, StopProviderError
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
		// once, including shell and custom-tool changes.
		if len(cfg.Gate.Commands) > 0 {
			if out, err := runGate(ctx, cfg.Gate.Commands, cfg.WorkDir); err != nil {
				r.Status, r.StopReason = StatusParked, StopGateFailedFinal
				r.Detail = out
				return r
			}
		}
		r.Status, r.StopReason = StatusPassed, StopFinished
	case len(res.Steps) >= maxSteps ||
		(cfg.Budget.MaxTokens > 0 && res.TotalUsage.TotalTokens >= cfg.Budget.MaxTokens):
		r.Status, r.StopReason = StatusParked, StopBudgetExhausted
		r.Detail = fmt.Sprintf("stopped after %d/%d steps, %d tokens", len(res.Steps), maxSteps, res.TotalUsage.TotalTokens)
	case len(state.changes) == 0:
		r.Status, r.StopReason = StatusIdle, StopIdle
		r.Detail = lastText(res)
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
