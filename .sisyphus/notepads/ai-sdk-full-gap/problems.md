# Scope Fidelity Check — Problems & Gaps

**Date**: 2026-05-03
**Verdict**: REJECT — 3 critical gaps

## GAP 1: ai-sdk-nats module BROKEN (plan violation)

`/work/projects/ai-sdk-nats/pkg/nats/transport.go` fails to compile:
```
undefined: chatui.UIMessage
undefined: chatui.StreamEvent
undefined: chatui.PartTypeText
undefined: chatui.UIMessagePart
```

Root cause: `pkg/ui/chat/` API surface was restructured during implementation.
Old types (`UIMessage`, `StreamEvent`, `UIMessagePart`, `PartTypeText`) were
replaced with new types (`Message`, `Chunk`, etc.) without backward compatibility.

Plan violations:
- Success Criteria: "go test ./... passes in ai-sdk-nats/" — FAILED
- Must NOT Have: "No breaking changes to existing API surface" — VIOLATED

## GAP 2: StreamObject capability MISSING

Plan T1 acceptance criteria: ObjectProvider interface should have both
`GenerateObject(ctx, req) (ObjectResult, error)` AND
`StreamObject(ctx, req) (ObjectStream, error)`.

Actual: Only `GenerateObject` + `Name()` methods on the Provider interface.
No `ObjectStream` type defined. No `StreamObject` in `pkg/core/`.

Plan T3 acceptance criteria: "StreamObject emits partial objects via channel"
— not implemented.

AGENTS.md was updated to match the incomplete implementation (only documents
`GenerateObject`), not what the plan specified.

## GAP 3: Module dependencies STALE

- `go mod tidy` in `ai-sdk/` produced changes to `go.mod` and `go.sum`
- `ai-sdk-examples/` has no `go.sum` file (though builds cleanly)

## Minor Architecture Notes

- `pkg/util/prompt.go` imports `pkg/chat` — violates AGENTS.md rule
  "util — standalone, stdlib only"
- `pkg/registry/registry.go` imports `pkg/agent` — subtle layer inversion
  (agent sits above services in architecture, registry is infrastructure below)
