Created utility helper packages:

- pkg/util/prompt.go: message formatting and convenience constructors for chat.Message
- pkg/util/tokenizer.go: simple token counting heuristic using regex (words + punctuation)
- pkg/error/errors.go: sentinel errors used across packages
- pkg/logger/logger.go: Logger interface with slog adapter and NoopLogger

Notes:
- Kept implementations minimal and stdlib-only to respect onion model
- Tokenizer is heuristic; counts word/punctuation tokens via regex; sufficient for rough accounting
- Tests added for basic smoke checks; more thorough property tests could be added later

### T24: Telemetry Middleware (pkg/middleware/telemetry.go)

Created `TelemetryMiddleware` that wraps `chat.Provider` with OpenTelemetry-compatible tracing:
- `Chat()`: span started before call, attributes set (`provider.name`, `model`, `messages.count`, `tools.count`), ended with defer. Errors recorded on span.
- `ChatStream()`: span started before call, wrapped stream (`telemetryStream`) ends span on Close. Errors from Stream.Next() recorded on span but do not end it prematurely.
- Uses `telemetry.Tracer` / `telemetry.Span` interfaces (no OTel SDK dependency).
- `telemetry.NoopTracer` already existed from T8 — reused here.
- 7 tests covering: name passthrough, successful chat, chat error, successful stream, stream setup error, mid-stream Next error, interface compliance check.
- Mock tracer/spans use sync.Mutex for safe concurrent access (race detector passes).

### T21: SSE Message Streaming (pkg/uimessage/sse/)

Verified and expanded test coverage for the SSE package:

**Verification findings:**
- `transform.go` already handles ALL 10 `core.StreamPartType` constants in its switch statement
- Each StreamPart type produces the correct `uimessage.Chunk` types for the UI wire protocol
- Writer correctly serialises chunks as `data: <json>\n\n` events

**Tests added (sse_test.go expanded from 137 → ~900 lines):**
- `TestWriterExhaustiveChunkTypes` — round-trips all 16 concrete chunk types through Write → Unmarshal
- Reasoning tests: `TestFromTextStreamReasoning`, `TestFromTextStreamMixedTextReasoning`, `TestFromTextStreamMultiStepReasoning`
- Error handling: `TestFromTextStreamError`, `TestFromTextStreamErrorFromString`, `TestFromTextStreamErrorNil`
- Abort: `TestFromTextStreamAbort`
- Warning: `TestFromTextStreamWarning`, `TestFromTextStreamWarningNil`
- Edge cases: `TestFromTextStreamEmpty`, `TestFromTextStreamNoFinishReason`, `TestFromTextStreamFinishReasonLength`, `TestFromTextStreamNoStepFinish`, `TestFromTextStreamStartChunkMetadata`
- Tool edge cases: `TestFromTextStreamToolCallInvalidJSON`, `TestFromTextStreamToolResultInvalidJSON`, `TestFromTextStreamToolResultError`, `TestFromTextStreamToolCallNil`, `TestFromTextStreamToolResultNil`
- Multi-step: `TestFromTextStreamMultiStepText`
- Context cancellation: `TestFromTextStreamContextCancel`, `TestFromTextStreamSendFailure`, `TestFromTextStreamPreCancelled`
- Writer tests: `TestApplyHeaders`, `TestNewWriter`, `TestWriteRaw`, `TestWriteRawWithFlush`, `TestPipe`, `TestPipeContextCancel`
- Writer error paths: `TestWriteChunkWriteError`, `TestWriteRawWriteError`, `TestPipeWriteError`, `TestWriteChunkMarshalError` (using `failingWriter` mock)

**Coverage results:**
- Overall: 91.2% (>90% target)
- `writer.go`: 100% across all functions
- `transform.go`: `FromTextStream` at 87.6% — remaining 12.4% are timing-dependent `send()`-failure paths (context cancelled while output buffer full), impractical to cover deterministically without injecting test hooks

**Modernization:**
- `SSEEventLines` updated to use `strings.SplitSeq` (Go 1.24+)
- Loop modernized to `for range 20` (Go 1.22+)

**Key behavioral insight:**
- Error and Abort stream parts are *inline events*, not stream terminators. The stream continues until the channel closes, at which point finish is sent. This means error/abort chunks may be followed by finish in the output — this is correct behavior.

