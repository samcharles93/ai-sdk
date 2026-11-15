# Agent missions

`Run` executes one mission with built-in tools, optional custom tools, budgets,
and a structured result. Orchestration between missions belongs to the caller.

## Checkpointed loops

`RunLoop` is the general agent loop underneath: it runs with only the caller's
tools — no built-in file or shell tools, no `WorkDir`, gate, preflight or notes.
When `LoopConfig.Journal` is set, every model call, tool call and compaction
takes a stable sequence position and is checkpointed through the caller's
`Journal` interface (ai-sdk owns no storage; the caller's run engine provides
persistence and leases):

- Before each call the loop looks the entry up at its position. A recorded
  entry is verified against its kind and a digest of the request or tool
  input, then replayed: the model response or tool result goes back to the
  model verbatim and nothing runs again.
- A fresh result is recorded before the loop continues. A rejected record
  (e.g. a lost lease) stops the loop at once with `ErrJournalRejected` and no
  further calls; a digest mismatch stops it with `ErrJournalDiverged`.
- A run killed at any point resumes from the journal with an identical
  transcript and no repeated tool calls. Cancellation mid-call records
  nothing, so the position simply re-runs on resume.
- Each tool call sees a position-derived idempotency key
  (`core.IdempotencyKey`), stable across replays, so tools with side effects
  can deduplicate. A tool can report `ErrOutcomeUnknown`; the loop records it
  as unknown, never retries it, and stops with `StopOutcomeUnknown`.
- Budgets span resumptions: token and step budgets include usage recorded in
  earlier attempts and refuse the next model call before it is made.
  `LoopResult.Messages` carries the full journal-derived transcript and
  `Usage` the totals. Hooks still fire for replayed calls, flagged with
  `core.Replayed(ctx)` so telemetry and cost are not double counted.

`Run` without a journal behaves exactly as before.

## Observe and intercept

Set `Config.ToolHooks` and `Config.ModelHooks` using the existing `core` hook
interfaces. Tool hooks cover built-ins, custom tools, `check`, `finish`, and
forced completion. Model hooks cover ordinary calls, summarisation, and forced
completion, including provider errors.

Caller Before hooks run first. Internal terminal and path guards then validate
the resulting call before execution. After hooks retain core's semantics: they
run even when execution was refused. Hooks that replace a result do not change
the run's internal completion or gate state.

## Path protection

`Config.ProtectPaths` refuses `write` and `edit` calls on matching paths. The
predicate receives the canonical WorkDir-relative path, so every spelling of one
file (`spec.txt`, `./spec.txt`, `@spec.txt`, an absolute path under WorkDir)
reaches the same decision; a path that escapes WorkDir arrives absolute.

This is a policy hook, not a sandbox. `shell` starts in WorkDir but is not
confined to it, and `Extra` tools run unrestricted — only calls named `write` or
`edit` are checked, whichever toolset they came from. Anything reachable through
shell or a custom tool bypasses path protection, so confine the process itself
(container, worktree, least-privilege user) when a mission must not escape
WorkDir.

## Gate checkpoints

When `Gate.Commands` is nonempty, the model gets a `check` tool. It can complete
coordinated edits across several files before calling `check`. Failed checks
report the command output; `read`, `find`, and `grep` remain available for repair.
Successful checks reset the consecutive-failure count.

Every `finish(status="passed")` triggers final validation, even if changes came
through shell or custom tools, or the model recorded no file changes. A failing
final gate parks the run. Calls remaining in a batch after completion or parking
are refused.

This replaces automatic validation after every write/edit. Custom system
prompts should tell the model to call `check` at coherent checkpoints. `check`
is reserved when gating is configured. Read-only missions still run configured
gate commands; callers must choose commands appropriate for that mission.

## Context compaction

With `Compact.Enabled` and a positive `ContextWindow`, the initial system and
user messages survive verbatim, including the mission, notes and preloaded
files. Compaction summarises older history and keeps up to two recent complete
assistant/tool turns. It may retain only the newest turn to leave summary space.

Ten percent of the window is reserved as output headroom. Summary size accounts
for retained messages, tool schemas and the summariser request itself. Requests
are checked before the first call, between turns, and before forced completion.
A failed summary preserves the original history only if it still fits.

If preserved context cannot fit, `Run` returns `ErrContextLimit` with
`StatusParked` and `StopContextLimit`. Reduce initial context or tool output, or
configure the correct larger window. Sizes are character-based estimates, not
provider tokenisation guarantees; image/audio token costs are not estimated.
