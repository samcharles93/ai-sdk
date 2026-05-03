# Architectural Decisions for agent_impl.go

## Circular Dependency (2026-05-03)

**Problem:** Task specified `pkg/core/agent_impl.go` returning `agent.StreamEvent`, but:
- `pkg/agent` already imports `pkg/core` (agent.go:8)
- `pkg/core` importing `pkg/agent` would create `agent → core → agent` cycle → compile error

**Decision:** Placed `RunAgent` in `pkg/agent/agent_impl.go` instead of `pkg/core/`.

**Rationale:**
- No circular import — agent already imports core
- Function returns native `agent.StreamEvent` as specified
- `core.StreamText` already handles the complete tool loop internally (maxSteps, tool execution, ctx cancellation, abort handling)
- `RunAgent` is a thin translation layer over StreamText, same pattern as `Agent.Run`

**Alternative considered:** Moving StreamEvent to core package → would require modifying agent package which was forbidden by MUST NOT.
