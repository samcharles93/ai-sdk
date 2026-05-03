# Learnings — HTTPTransport

## Interface Alignment
- Plan used `StreamEvent` and `UIMessage` type names, but actual codebase uses `Chunk` (from `uimessage`) and `Message` (re-exported).
- Transport interface uses `SendMessages(ctx, chatID, messages, opts SendOptions)` — match the actual interface, not plan prose.

## SSE Parsing
- SSE wire format: `data: <json>\n\n` per `pkg/uimessage/sse/writer.go`
- Use `uimessage.UnmarshalChunk([]byte)` to decode each `data:` payload — dispatches on the `"type"` discriminator
- Empty lines between events (SSE spec) — handled by `if line == "" { continue }`

## Context Cancellation
- `http.NewRequestWithContext` handles cancellation of the HTTP request
- Goroutine select `<-ctx.Done()` stops emitting to channel on cancel
- `defer resp.Body.Close()` in goroutine ensures cleanup

## Modern Go
- `maps.Copy(reqBody, opts.Body)` preferred over manual loop (Go 1.21+)
- Go 1.26.2 supports all modern stdlib improvements
