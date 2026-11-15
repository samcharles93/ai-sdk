package agentloop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
)

// StatusCompleted means a [RunLoop] run ended because the model stopped
// requesting tool calls.
const StatusCompleted Status = "completed"

// LoopConfig configures a [RunLoop] call: a general agent loop with no
// coding assumptions. It runs with only the caller's tools — no built-in
// file or shell tools, no WorkDir, gate, preflight or notes; the coding
// mission in [Run] is one preset of caller-supplied tools and prompts.
//
// When Journal is set, every model call, tool call and compaction is
// checkpointed through it: the loop looks the entry up at its position,
// replays it when present, and records fresh results before continuing. A
// run killed at any point resumes from the journal with an identical
// transcript and no repeated tool calls. When Journal is nil the loop runs
// plain, with no checkpointing.
type LoopConfig struct {
	Provider  chat.Provider
	Model     string
	ModelInfo chat.ModelInfo

	// System is prepended as a system message; Messages is the initial
	// history; Prompt, when set, is appended as a final user message.
	System   string
	Messages []chat.Message
	Prompt   string

	// Tools is the caller's whole toolset.
	Tools      core.ToolSet
	ToolChoice *chat.ToolChoice

	Temperature     float32
	MaxTokens       int
	ProviderOptions map[string]any

	// Budget bounds the run. MaxSteps and MaxTokens include usage and steps
	// recorded in earlier attempts, so a resumed run refuses the next model
	// call before it is made. WallClock wraps the context.
	Budget Budget

	// Journal, when non-nil, receives every model call, tool call and
	// compaction. ai-sdk owns no storage; the caller's run engine implements
	// Journal, typically over durable storage with a lease.
	Journal Journal
	// RunID names the run inside idempotency keys (see core.IdempotencyKey);
	// "run" when empty.
	RunID string

	// Stream makes model calls through ChatStream, draining each stream
	// completely and recording the assembled response.
	Stream bool

	// Compact enables automatic history compaction; each compaction is a
	// journalled entry, so replay rebuilds the compacted history without
	// calling the model again.
	Compact CompactionConfig

	// StopWhen, when non-nil, is evaluated between steps with all steps so
	// far, including replayed ones.
	StopWhen core.StopCondition

	// ToolHooks and ModelHooks fire for real calls and for replayed calls
	// alike; replayed calls carry core.WithReplayed in their context so
	// telemetry and cost are not double counted. Before-hooks run ahead of
	// recording; after-hooks see the recorded result. A panicking model
	// hook stops the loop with ErrModelHookPanic.
	ToolHooks  []core.ToolHook
	ModelHooks []core.ModelHook

	Logger *slog.Logger
}

// LoopResult is the outcome of a [RunLoop] call. Messages is the full
// journal-derived transcript (system, initial history, assistant turns and
// tool results), and Usage totals every recorded call, including replayed
// ones from earlier attempts.
type LoopResult struct {
	Status     Status            `json:"status"`
	StopReason string            `json:"stop_reason"`
	Detail     string            `json:"detail,omitempty"`
	Messages   []chat.Message    `json:"messages"`
	Steps      []core.StepResult `json:"steps,omitempty"`
	ToolCalls  []core.ToolCall   `json:"tool_calls,omitempty"`
	// ToolResults holds the outcome of every tool call, replayed or fresh.
	ToolResults  []core.ToolResult `json:"tool_results,omitempty"`
	Usage        chat.Usage        `json:"usage"`
	FinishReason core.FinishReason `json:"finish_reason,omitempty"`
	// ReplayedEntries counts how many journal entries this run replayed
	// instead of making a live call.
	ReplayedEntries int `json:"replayed_entries"`
}

// RunLoop drives a tool-using agent loop in which every model call and tool
// call takes a stable sequence position and, when cfg.Journal is set, is
// looked up and recorded through the journal. Tool calls run one at a time
// in the model's call order, so positions never depend on timing; use
// core.GenerateText directly when parallel tool execution matters more than
// checkpointing.
//
// The returned error reports infrastructure failures: a nil provider,
// journal lookup failures, ErrJournalRejected, ErrJournalDiverged,
// ErrModelHookPanic, and final provider errors (which are also recorded).
// Cancellation and budgets are mission-level outcomes expressed in
// LoopResult.Status.
func RunLoop(ctx context.Context, cfg LoopConfig) (LoopResult, error) {
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if cfg.Provider == nil {
		return LoopResult{}, core.ErrNoProvider
	}
	if cfg.Budget.WallClock > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Budget.WallClock)
		defer cancel()
	}

	runID := cfg.RunID
	if runID == "" {
		runID = "run"
	}
	st := &journalState{
		provider: cfg.Provider,
		journal:  cfg.Journal,
		stream:   cfg.Stream,
		runID:    runID,
		hooks:    cfg.ModelHooks,
		log:      log,
	}

	messages := make([]chat.Message, 0, len(cfg.Messages)+2)
	if cfg.System != "" {
		messages = append(messages, chat.Message{Role: chat.RoleSystem, Content: cfg.System})
	}
	messages = append(messages, cfg.Messages...)
	if cfg.Prompt != "" {
		messages = append(messages, chat.Message{Role: chat.RoleUser, Content: cfg.Prompt})
	}
	wireTools := toolsToChatSorted(cfg.Tools)
	maxSteps := cfg.Budget.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 40
	}

	// Compaction runs through the journalled provider, so each summary call
	// takes a position and is replayed on resume.
	compact := compactorForLoop(cfg, st, cfg.Model, cfg.ModelInfo, cfg.Tools, log)

	var (
		steps      []core.StepResult
		lastReason core.FinishReason
	)
	finish := func(status Status, reason, detail string, err error) (LoopResult, error) {
		res := LoopResult{
			Status:          status,
			StopReason:      reason,
			Detail:          detail,
			Messages:        messages,
			Steps:           steps,
			Usage:           st.spent,
			FinishReason:    lastReason,
			ReplayedEntries: st.replayed,
		}
		for _, s := range steps {
			res.ToolCalls = append(res.ToolCalls, s.ToolCalls...)
			res.ToolResults = append(res.ToolResults, s.ToolResults...)
		}
		if err != nil && ctx.Err() == nil {
			return res, err
		}
		return res, nil
	}
	parkedCtx := func() (LoopResult, error) {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return finish(StatusParked, StopTimedOut, ctx.Err().Error(), nil)
		}
		return finish(StatusParked, StopCanceled, ctx.Err().Error(), nil)
	}

	for stepNum := 0; ; stepNum++ {
		if err := ctx.Err(); err != nil {
			return parkedCtx()
		}
		// Budgets span resumptions: refuse the next model call before it is
		// made, counting steps and usage recorded in earlier attempts.
		if len(steps) >= maxSteps ||
			(cfg.Budget.MaxTokens > 0 && effectiveTotal(st.spent) >= cfg.Budget.MaxTokens) {
			return finish(StatusParked, StopBudgetExhausted, fmt.Sprintf(
				"stopped after %d/%d steps, %d tokens", len(steps), maxSteps, effectiveTotal(st.spent)), nil)
		}
		if compact != nil {
			next, err := compact.onStep(withEntryKind(ctx, EntryCompact), messages)
			if err != nil {
				if errors.Is(err, ErrContextLimit) {
					return finish(StatusParked, StopContextLimit, err.Error(), err)
				}
				return finish(StatusParked, StopProviderError, err.Error(), err)
			}
			if next != nil {
				messages = next
			}
		}

		req := chat.Request{
			Model:           cfg.Model,
			Messages:        messages,
			Tools:           wireTools,
			ToolChoice:      cfg.ToolChoice,
			MaxTokens:       cfg.MaxTokens,
			Temperature:     cfg.Temperature,
			ProviderOptions: cfg.ProviderOptions,
			ModelInfo:       cfg.ModelInfo,
		}
		resp, err := st.callModel(ctx, req, EntryModel)
		if err != nil {
			switch {
			case errors.Is(err, ErrJournalRejected):
				return finish(StatusParked, StopJournalRejected, err.Error(), err)
			case errors.Is(err, ErrJournalDiverged):
				return finish(StatusParked, StopJournalDiverged, err.Error(), err)
			case errors.Is(err, ErrModelHookPanic):
				return finish(StatusParked, StopModelHookPanic, err.Error(), err)
			case ctx.Err() != nil:
				return parkedCtx()
			default:
				return finish(StatusParked, StopProviderError, err.Error(), err)
			}
		}

		calls := toCoreToolCalls(resp.ToolCalls)
		reason := mapFinishReason(resp.FinishReason)
		if len(calls) > 0 && reason != core.FinishReasonToolCalls {
			reason = core.FinishReasonToolCalls
		}
		steps = append(steps, core.StepResult{
			StepNumber:   stepNum,
			FinishReason: reason,
			Text:         resp.Content,
			Parts:        resp.Parts,
			Reasoning:    partsReasoning(resp.Parts),
			ToolCalls:    calls,
			Usage:        resp.Usage,
			Warnings:     resp.Warnings,
		})
		messages = append(messages, assistantMessage(resp))
		lastReason = reason

		for _, call := range calls {
			res, msg, terr := st.runTool(ctx, call, cfg.Tools, cfg.ToolHooks)
			if terr != nil {
				if errors.Is(terr, ErrOutcomeUnknown) {
					// The unknown outcome is recorded; append it so the
					// transcript matches a resume that stops the same way.
					steps[len(steps)-1].ToolResults = append(steps[len(steps)-1].ToolResults, res)
					messages = append(messages, msg)
					return finish(StatusParked, StopOutcomeUnknown, terr.Error(), terr)
				}
				switch {
				case errors.Is(terr, ErrJournalRejected):
					return finish(StatusParked, StopJournalRejected, terr.Error(), terr)
				case errors.Is(terr, ErrJournalDiverged):
					return finish(StatusParked, StopJournalDiverged, terr.Error(), terr)
				case ctx.Err() != nil:
					return parkedCtx()
				default:
					return finish(StatusParked, StopProviderError, terr.Error(), terr)
				}
			}
			steps[len(steps)-1].ToolResults = append(steps[len(steps)-1].ToolResults, res)
			messages = append(messages, msg)
		}

		if len(calls) == 0 {
			return finish(StatusCompleted, StopFinished, "", nil)
		}
		if cfg.StopWhen != nil && cfg.StopWhen(steps) {
			return finish(StatusParked, StopConditionFired, "", nil)
		}
	}
}

// journalState owns the position counter and the journalling around every
// model and tool call. It implements chat.Provider so compaction (which
// calls the model through core.GenerateText) is journalled too.
type journalState struct {
	provider chat.Provider
	journal  Journal
	stream   bool
	runID    string
	hooks    []core.ModelHook
	log      *slog.Logger

	next     int64
	spent    chat.Usage
	replayed int
}

func (s *journalState) Name() string { return s.provider.Name() }

// Chat journals the call with the entry kind carried by ctx (compaction tags
// its context; anything else is a plain model entry).
func (s *journalState) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	return s.callModel(ctx, req, entryKindFromContext(ctx))
}

func (s *journalState) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	return s.provider.ChatStream(ctx, req)
}

// callModel performs one journalled model call at the next position: replay
// the recorded entry when the journal holds one, otherwise make the live
// call and record the result before returning. A final provider failure is
// recorded too (transient failures are the retry middleware's job, at the
// same position); a cancelled call records nothing.
func (s *journalState) callModel(ctx context.Context, req chat.Request, kind EntryKind) (chat.Response, error) {
	pos := s.next
	s.next++
	digest := digestRequest(req)

	if s.journal != nil {
		entry, err := s.journal.Lookup(ctx, pos)
		if err != nil {
			return chat.Response{}, fmt.Errorf("agentloop: journal lookup at position %d: %w", pos, err)
		}
		if entry != nil {
			if entry.Kind != kind || entry.Digest != digest {
				return chat.Response{}, fmt.Errorf(
					"%w: position %d holds kind %q with digest %s, replay expects kind %q with digest %s",
					ErrJournalDiverged, pos, entry.Kind, entry.Digest, kind, digest,
				)
			}
			s.replayed++
			s.spent = addUsage(s.spent, entry.Usage)
			resp := entry.response()
			rctx := core.WithReplayed(ctx)
			if err := fireModelStarted(rctx, s.hooks, req); err != nil {
				return chat.Response{}, err
			}
			usage := resp.Usage
			if err := fireModelFinished(rctx, s.hooks, req, &resp, nil, 0, &usage); err != nil {
				return chat.Response{}, err
			}
			if entry.Error != "" {
				return resp, errors.New(entry.Error)
			}
			return resp, nil
		}
	}

	if err := fireModelStarted(ctx, s.hooks, req); err != nil {
		return chat.Response{}, err
	}
	start := time.Now()
	var (
		resp chat.Response
		err  error
	)
	if s.stream {
		resp, err = drainStream(ctx, s.provider, req)
	} else {
		resp, err = s.provider.Chat(ctx, req)
	}
	latency := time.Since(start)
	var respPtr *chat.Response
	var usagePtr *chat.Usage
	if err == nil {
		respCopy := resp
		respPtr = &respCopy
		usageCopy := resp.Usage
		usagePtr = &usageCopy
	}
	if ferr := fireModelFinished(ctx, s.hooks, req, respPtr, err, latency, usagePtr); ferr != nil {
		return chat.Response{}, ferr
	}
	if ctx.Err() != nil {
		// Cancelled mid-call: leave no entry, the position re-runs on resume.
		return resp, ctx.Err()
	}
	s.spent = addUsage(s.spent, resp.Usage)
	entry := Entry{
		Position:     pos,
		Kind:         kind,
		Digest:       digest,
		FinishReason: resp.FinishReason,
		Usage:        resp.Usage,
	}
	if err != nil {
		entry.Error = err.Error()
	} else {
		msg := assistantMessage(resp)
		entry.Message = &msg
	}
	if s.journal != nil {
		if rerr := s.journal.Record(ctx, entry); rerr != nil {
			return resp, fmt.Errorf("%w: position %d: %w", ErrJournalRejected, pos, rerr)
		}
	}
	return resp, err
}

// runTool performs one journalled tool call at the next position. The tool
// sees a context idempotency key derived from the position. Before-hooks run
// ahead of recording; the result is recorded before after-hooks see it. A
// rejected record stops the loop with no further calls.
func (s *journalState) runTool(ctx context.Context, call core.ToolCall, set core.ToolSet, hooks []core.ToolHook) (core.ToolResult, chat.Message, error) {
	pos := s.next
	s.next++
	input := call.Input
	digest := digestToolCall(call.ToolName, input)

	if s.journal != nil {
		entry, err := s.journal.Lookup(ctx, pos)
		if err != nil {
			return core.ToolResult{}, chat.Message{}, fmt.Errorf("agentloop: journal lookup at position %d: %w", pos, err)
		}
		if entry != nil {
			if entry.Kind != EntryTool || entry.ToolName != call.ToolName || entry.Digest != digest {
				return core.ToolResult{}, chat.Message{}, fmt.Errorf(
					"%w: position %d holds kind %q tool %q with digest %s, replay expects tool %q with digest %s",
					ErrJournalDiverged, pos, entry.Kind, entry.ToolName, entry.Digest, call.ToolName, digest,
				)
			}
			s.replayed++
			res := core.ToolResult{ToolCallID: entry.ToolCallID, ToolName: entry.ToolName, Output: entry.Output, Error: entry.Error}
			// Hooks still fire, flagged as replay with the same idempotency
			// key the live call saw; the journal is authoritative, so hook
			// results are not applied.
			rctx := core.WithIdempotencyKey(core.WithReplayed(ctx), fmt.Sprintf("%s:%d", s.runID, pos))
			replayed := call
			_, _ = runBeforeHooks(rctx, hooks, &replayed)
			runAfterHooks(rctx, hooks, replayed, &res)
			msg := toolMessage(call, res)
			if entry.Unknown {
				return res, msg, fmt.Errorf("%w: tool %q at position %d", ErrOutcomeUnknown, call.ToolName, pos)
			}
			return res, msg, nil
		}
	}

	tctx := core.WithIdempotencyKey(ctx, fmt.Sprintf("%s:%d", s.runID, pos))
	res := core.ToolResult{ToolCallID: call.ToolCallID, ToolName: call.ToolName}
	var execErr error
	skip, deny := runBeforeHooks(tctx, hooks, &call)
	switch {
	case deny != nil:
		res.Error = deny.Error()
	case skip != nil:
		res.Output, res.Error = skip.Output, skip.Error
	default:
		tool, ok := set[call.ToolName]
		switch {
		case !ok || tool == nil:
			res.Error = fmt.Errorf("%w: %q", core.ErrToolNotFound, call.ToolName).Error()
		case tool.Execute == nil:
			res.Error = fmt.Errorf("%w: tool %q has no Execute function", core.ErrToolExecutionFailed, call.ToolName).Error()
		default:
			out, err := safeExecute(tctx, call, tool)
			execErr = err
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Output = out
			}
		}
	}
	if ctx.Err() != nil {
		// Cancelled mid-call: leave no entry, the position re-runs on resume.
		return res, chat.Message{}, ctx.Err()
	}
	entry := Entry{
		Position:   pos,
		Kind:       EntryTool,
		Digest:     digest,
		ToolName:   call.ToolName,
		ToolCallID: call.ToolCallID,
		Input:      input,
		Output:     res.Output,
		Error:      res.Error,
	}
	if errors.Is(execErr, ErrOutcomeUnknown) {
		entry.Unknown = true
	}
	if s.journal != nil {
		if rerr := s.journal.Record(ctx, entry); rerr != nil {
			return res, chat.Message{}, fmt.Errorf("%w: position %d: %w", ErrJournalRejected, pos, rerr)
		}
	}
	runAfterHooks(tctx, hooks, call, &res)
	msg := toolMessage(call, res)
	if entry.Unknown {
		// Recorded as unknown: never retried, and the run stops distinctly.
		return res, msg, fmt.Errorf("%w: tool %q at position %d", ErrOutcomeUnknown, call.ToolName, pos)
	}
	return res, msg, nil
}

// fireModelStarted runs ModelCallStarted on every hook. A panic stops the
// loop with ErrModelHookPanic rather than being swallowed.
func fireModelStarted(ctx context.Context, hooks []core.ModelHook, req chat.Request) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: ModelCallStarted: %v", ErrModelHookPanic, r)
		}
	}()
	for _, h := range hooks {
		h.ModelCallStarted(ctx, req)
	}
	return nil
}

// fireModelFinished runs ModelCallFinished on every hook; see fireModelStarted.
func fireModelFinished(ctx context.Context, hooks []core.ModelHook, req chat.Request, resp *chat.Response, err error, latency time.Duration, usage *chat.Usage) (perr error) {
	defer func() {
		if r := recover(); r != nil {
			perr = fmt.Errorf("%w: ModelCallFinished: %v", ErrModelHookPanic, r)
		}
	}()
	for _, h := range hooks {
		h.ModelCallFinished(ctx, req, resp, err, latency, usage)
	}
	return nil
}

// runBeforeHooks runs the BeforeToolExecute chain. A panicking hook denies
// the call with a ToolPanicError, as in core.
func runBeforeHooks(ctx context.Context, hooks []core.ToolHook, call *core.ToolCall) (*core.Skip, error) {
	for _, h := range hooks {
		var (
			skip *core.Skip
			err  error
		)
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = &core.ToolPanicError{ToolName: call.ToolName, Phase: "before", Value: r, Stack: debug.Stack()}
				}
			}()
			skip, err = h.BeforeToolExecute(ctx, call)
		}()
		if err != nil || skip != nil {
			return skip, err
		}
	}
	return nil, nil
}

// runAfterHooks runs the AfterToolExecute chain; a panicking hook overrides
// the result error, as in core.
func runAfterHooks(ctx context.Context, hooks []core.ToolHook, call core.ToolCall, res *core.ToolResult) {
	for _, h := range hooks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					res.Error = (&core.ToolPanicError{ToolName: call.ToolName, Phase: "after", Value: r, Stack: debug.Stack()}).Error()
				}
			}()
			h.AfterToolExecute(ctx, call, res)
		}()
	}
}

// safeExecute runs tool.Execute under panic containment: a panicking tool
// becomes a failed tool call carrying the panic text.
func safeExecute(ctx context.Context, call core.ToolCall, tool *core.Tool) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &core.ToolPanicError{ToolName: call.ToolName, Phase: "during", Value: r, Stack: debug.Stack()}
		}
	}()
	return tool.Execute(ctx, call.Input)
}

// drainStream consumes a whole provider stream and assembles the response,
// so a streamed call is recorded exactly like a non-streamed one.
func drainStream(ctx context.Context, provider chat.Provider, req chat.Request) (chat.Response, error) {
	stream, err := provider.ChatStream(ctx, req)
	if err != nil {
		return chat.Response{}, err
	}
	defer stream.Close()
	var (
		text, reasoning string
		deltas          []chat.ToolCallDelta
		usage           chat.Usage
		reason          string
		warnings        []chat.Warning
		meta            map[string]any
	)
	partial := func(err error) (chat.Response, error) {
		return chat.Response{
			Role:             chat.RoleAssistant,
			Content:          text,
			ToolCalls:        chat.AssembleToolCalls(deltas),
			FinishReason:     reason,
			Usage:            usage,
			Warnings:         warnings,
			ProviderMetadata: meta,
		}, err
	}
	for {
		chunk, cerr := stream.Next(ctx)
		if errors.Is(cerr, io.EOF) {
			break
		}
		if cerr != nil {
			return partial(cerr)
		}
		text += chunk.Delta
		reasoning += chunk.ReasoningDelta
		deltas = append(deltas, chunk.ToolCallDeltas...)
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
		if chunk.FinishReason != "" {
			reason = chunk.FinishReason
		}
		warnings = append(warnings, chunk.Warnings...)
		if chunk.ProviderMetadata != nil {
			meta = chunk.ProviderMetadata
		}
		if chunk.Done {
			break
		}
	}
	var parts chat.Parts
	if reasoning != "" {
		parts = append(parts, chat.ReasoningPart{Text: reasoning})
	}
	if text != "" {
		parts = append(parts, chat.TextPart{Text: text})
	}
	resp, perr := partial(nil)
	resp.Parts = parts
	return resp, perr
}

// compactorForLoop builds the compaction hook for RunLoop. The provider is
// the journalState, so the summarisation call is journalled, and modelHooks
// stay nil because the journalled provider fires them.
func compactorForLoop(cfg LoopConfig, provider chat.Provider, model string, info chat.ModelInfo, tools core.ToolSet, log *slog.Logger) *compactor {
	if !cfg.Compact.Enabled {
		return nil
	}
	return &compactor{
		provider:  provider,
		model:     model,
		modelInfo: info,
		tools:     tools,
		cfg:       cfg.Compact.normalised(),
		log:       log,
	}
}

type entryKindContextKey struct{}

func withEntryKind(ctx context.Context, kind EntryKind) context.Context {
	return context.WithValue(ctx, entryKindContextKey{}, kind)
}

func entryKindFromContext(ctx context.Context) EntryKind {
	if k, ok := ctx.Value(entryKindContextKey{}).(EntryKind); ok {
		return k
	}
	return EntryModel
}

// toolsToChatSorted converts the toolset to wire tools in name order, so the
// request digest is canonical (map iteration order is not).
func toolsToChatSorted(set core.ToolSet) []chat.Tool {
	if len(set) == 0 {
		return nil
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]chat.Tool, 0, len(set))
	for _, name := range names {
		t := set[name]
		if t == nil {
			continue
		}
		out = append(out, chat.Tool{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	return out
}

func toCoreToolCalls(in []chat.ToolCall) []core.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.ToolCall, len(in))
	for i, c := range in {
		out[i] = core.ToolCall{ToolCallID: c.ID, ToolName: c.Name, Input: c.Arguments}
	}
	return out
}

func mapFinishReason(s string) core.FinishReason {
	switch s {
	case "", "stop", "end_turn":
		return core.FinishReasonStop
	case "length", "max_tokens":
		return core.FinishReasonLength
	case "content_filter", "content-filter", "safety":
		return core.FinishReasonContentFilter
	case "tool_calls", "tool-calls", "tool_use":
		return core.FinishReasonToolCalls
	case "error":
		return core.FinishReasonError
	default:
		return core.FinishReasonOther
	}
}

func partsReasoning(parts chat.Parts) string {
	var out strings.Builder
	for _, p := range parts {
		if rp, ok := p.(chat.ReasoningPart); ok {
			out.WriteString(rp.Text)
		}
	}
	return out.String()
}

func assistantMessage(resp chat.Response) chat.Message {
	return chat.Message{
		Role:            chat.RoleAssistant,
		Content:         resp.Content,
		Parts:           resp.Parts,
		ToolCalls:       resp.ToolCalls,
		ProviderOptions: resp.ProviderMetadata,
	}
}

func toolMessage(call core.ToolCall, res core.ToolResult) chat.Message {
	content := res.Output
	if res.Error != "" {
		content = res.Error
	}
	return chat.Message{
		Role:       chat.RoleTool,
		Content:    content,
		Name:       call.ToolName,
		ToolCallID: call.ToolCallID,
	}
}
