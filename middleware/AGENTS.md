# middleware

```go
type ChatMiddleware func(next chat.Provider) chat.Provider
func ChainChat(ms ...ChatMiddleware) ChatMiddleware // first middleware is outermost
```

- `TelemetryMiddleware` — wraps provider with OTel spans for Chat/ChatStream
- `ChatRequestHook` / `ChatResponseHook` — interception points
- Generation domains have per-domain wrappers (`retry_image`,
  `telemetry_video`, `circuitbreaker_object`, `rerank`, etc.); image editing has
  `*_image_edit` wrappers.

Speech streaming uses `ChainSpeechStream`: end telemetry spans on Close,
guard establishment only with the circuit breaker, and do not retry streams.
Keep optional image editing and speech streaming separate from base wrappers.
