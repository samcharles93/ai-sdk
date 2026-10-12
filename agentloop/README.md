# Agent missions

`Run` executes one mission with built-in tools, optional custom tools, budgets,
and a structured result. Orchestration between missions belongs to the caller.

## Observe and intercept

Set `Config.ToolHooks` and `Config.ModelHooks` using the existing `core` hook
interfaces. Tool hooks cover built-ins, custom tools, `check`, `finish`, and
forced completion. Model hooks cover ordinary calls, summarisation, and forced
completion, including provider errors.

Caller Before hooks run first. Internal terminal and path guards then validate
the resulting call before execution. After hooks retain core's semantics: they
run even when execution was refused. Hooks that replace a result do not change
the run's internal completion or gate state.

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
