# Learnings from agent_impl.go

## StreamText is the complete tool-loop engine

`core.StreamText` handles the ENTIRE agent orchestration internally:
- Tool execution (`executeToolCalls` in loop.go)
- Feeding tool results back as messages
- MaxSteps enforcement (via `effectiveStopCondition` → `StepCountIs`)
- Context cancellation (emits `StreamPartAbort`)
- Step start/finish boundaries

The `RunAgent` function (and `Agent.Run`) are just translation layers that convert `StreamPart` → `StreamEvent`.

## Package dependency chain

```
pkg/chat (domain interface) ← pkg/core (services) ← pkg/agent (user-facing)
```

## Translation function is private

`translate()` in agent.go handles all `StreamPart` types → `StreamEvent` mapping. It's package-private and reused by both `Agent.Run` and `RunAgent`.
