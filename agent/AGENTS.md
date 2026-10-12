# agent

```tree
agent/
  agent.go            Agent struct, StreamEvent types, translate()
  agent_impl.go       RunAgent function (convenience API)
  doc.go              Package documentation with usage examples
```

**Key concepts:**

- `Agent` struct — Provider, Model, System, Tools, MaxSteps, Temperature,
  MaxTokens
- `Agent.Run(ctx, prompt)` — returns `<-chan StreamEvent`
- `StreamEvent` — Type-based event dispatch (TextDelta, ToolCall, ToolResult,
  Finish, Error, Abort)

The agent does NOT execute tools itself — `core.StreamText` handles the full tool
loop internally. The agent concentrates on event translation and lifecycle
management.
