
## Gap 1 Fix — Backward-compat layer for pkg/ui/chat

### What happened
The `ai-sdk-nats` module at `/work/projects/ai-sdk-nats/` imports `pkg/ui/chat` and uses old type names (`UIMessage`, `StreamEvent`, `UIMessagePart`, `PartType`, `ToolResult`). These types were removed during a refactor to the UI message protocol.

### Fix
Created `pkg/ui/chat/compat.go` with:
- `UIMessage = Message` (type alias to current `uimessage.Message`)
- `StreamEvent` — struct with `Type PartType`, `Part UIMessagePart`, `Error error`
- `UIMessagePart` — flat struct with `Type`, `Text`, `Reasoning`, `ToolCall *ToolCall`, `ToolResult *ToolResult`
- `PartType` — string type with `PartTypeText`, `PartTypeReasoning`, `PartTypeToolCall`, `PartTypeToolResult`, `PartTypeStepStart`
- `ToolResult` — struct with `ToolCallID`, `ToolName`, `Output any`, `IsError bool`

### What already existed
- `ToolCall` — already in `options.go` (no need to re-create)
- `Message` — already aliased to `uimessage.Message` in `types.go`

### Verification
- `go build ./pkg/ui/chat/` — pass
- `go build ./...` in ai-sdk-nats — pass
- `go vet` both projects — pass
- LSP diagnostics on compat.go — clean
