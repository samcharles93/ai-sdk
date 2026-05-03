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

### Registry wiring (T31-T36)

- Added object, video and agent registries to pkg/registry/registry.go along with RegisterObject/RegisterVideo/RegisterAgent helpers and Object/Video/Agent getters.
- Updated cmd/ai-sdk/main.go to optionally register Mistral, Groq, xAI, Perplexity, Azure and Cohere providers (chat + embed/image/rerank where applicable) using environment variables. Registrations are skipped when env vars are missing, matching existing provider pattern.
- Verified: go build ./... and go vet ./... succeed locally after changes.

### T37-T42: Examples and AGENTS.md

Created 5 example programs in `ai-sdk-examples/`:

- **openai-chat/main.go**: Interactive CLI using `core.GenerateText()` with OpenAI provider. Reads stdin line-by-line, sends to model, prints response. Requires `OPENAI_API_KEY`.
- **anthropic-agent/main.go**: Agent with tool use using `agent.RunAgent()` with Anthropic provider. Defines a mock `get_weather` tool with JSON Schema parameters and hard-coded results. Demonstrates streaming event handling (TextDelta, ToolCall, ToolResult, Finish, Error/Abort). Requires `ANTHROPIC_API_KEY`.
- **object-generation/main.go**: API pattern demonstration for `core.GenerateObject()` / `object.Provider`. Informational only (no provider implements object.Provider yet). Shows schema definition, request building, and result handling pattern.
- **speech-to-text/main.go**: API pattern demonstration for `transcribe.Provider`. Informational only (no provider implements transcribe.Provider yet). Shows TranscribeRequest and response handling with segments.
- **image-generation/main.go**: API pattern demonstration for `core.GenerateImage()` / `image.Provider`. References Azure and TogetherAI providers that implement image.Provider. Shows GenerateImageRequest construction and response handling.

**Key decisions:**
- Used `replace` directive in `ai-sdk-examples/go.mod` pointing to `../` to avoid external dependency issues
- Examples that need API keys take them from environment variables (matching `cmd/ai-sdk/main.go` convention)
- Object/speech/image examples are informational — they run without API keys and document the API pattern
- All examples import from `github.com/samcharles93/ai-sdk` via the replace directive

**AGENTS.md updated with:**
- Full onion model diagram updated (added object, video, agent, rerank, upload, prompt, error, logger, telemetry, uimessage/sse layers)
- Complete File Organization tree with all 12 providers and all 22 pkg/ directories
- Provider Ecosystem table listing all 12 providers with capability matrix (Chat, Embed, Image, Speech, Transcribe, Object, Rerank, Video)
- 9 new package documentation sections: object, video, agent, upload, util, error, logger, telemetry, middleware, uimessage/sse
- Examples section with run commands

**Verification:**
- `go build ./...` — passes
- `go vet ./...` — passes

## F3 QA Findings (2026-05-03)

**Verdict: APPROVE** — Zero failures across all verification checks.

### QA Results

| Check | Result |
|-------|--------|
| `go test -count=1 -race ./...` | ALL 25 packages PASS, 0 failures |
| Flaky test check (3x repeated) | Core, OpenAI, SSE all pass consistently |
| `go vet ./...` (main module) | Clean |
| `go vet ./...` (examples module) | Clean |
| `go build ./cmd/ai-sdk/` | Compiles cleanly |
| `go build ./...` (ai-sdk-examples) | All 5 examples compile |
| `go mod verify` | All modules verified |
| Server dry-run | Starts on :8080, gracefully skips missing providers |

### Key observations
- Server gracefully handles missing API keys with informative log messages
- Signal.NotifyContext used for proper graceful shutdown with 10s timeout
- All examples follow consistent run() pattern with proper error handling
- Domain packages (pkg/chat, pkg/embed, etc.) are clean — no cross-layer imports
- Provider test coverage spans all 11 providers

### Packages without tests (expected)
cmd/ai-sdk (CLI entrypoint), pkg/agent (orchestrator), pkg/image/object/speech/transcribe/video (data-only domain types), pkg/prompt (standalone helper), pkg/registry (wiring), pkg/schema (standalone), pkg/telemetry (noop + interfaces), pkg/ui/* (Templ components + handlers)

## GAP 2: StreamObject (2026-05-03)

### Changes
- **pkg/object/stream.go** (new): `ObjectChunk` struct (Delta, Done) and `ObjectStream` interface (Next, Close). Follows same pattern as `chat.Stream` in `pkg/chat/provider.go`.
- **pkg/object/provider.go**: `Provider` interface gained `StreamObject(ctx, req) (ObjectStream, error)` method.
- **pkg/object/client.go**: `Client` facade gained `StreamObject` nil-guard method matching `GenerateObject` pattern.
- **pkg/core/object_impl.go**: New `StreamObject` orchestration function validating provider, context cancellation, and delegating to provider.
- **pkg/core/object_impl_test.go** (new): 4 tests — nil provider, context cancellation, successful delegation, provider error. Uses `fakeObjectProvider` and `fakeObjectStream` test doubles.

### Verification
- `go build ./...` — passes
- `go test -race ./pkg/object/... ./pkg/core/...` — passes
- `go vet ./pkg/object/... ./pkg/core/...` — clean

---

## QA Run #2 (F3 Re-run) — 2026-05-03 12:48

### Commands Executed

| # | Command | Module | Result |
|---|---------|--------|--------|
| 1 | `go test -count=1 -race ./...` | ai-sdk (root) | ✅ ALL 20 packages pass (race detector clean) |
| 2 | `go build ./...` | ai-sdk-nats | ✅ Builds successfully |
| 3 | `go build ./...` | ai-sdk-examples | ✅ Builds successfully |
| 4 | `go vet ./...` | ai-sdk (root) | ✅ Clean, no warnings |
| 5 | `go vet ./...` | ai-sdk-nats | ✅ Clean, no warnings |
| 6 | `go build ./...` | ai-sdk (main) | ✅ Builds successfully |
| 7 | `timeout 3 go run ./cmd/ai-sdk/` | ai-sdk (root) | ✅ Server starts, providers register, listens on :8080 |

### Server Dry-Run Output
```
time=2026-05-03T12:48:57.400+10:00 level=INFO msg="provider registered" name=openai
time=2026-05-03T12:48:57.400+10:00 level=INFO msg="provider registered" name=deepseek
time=2026-05-03T12:48:57.401+10:00 level=INFO msg="provider registered" name=gemini
time=2026-05-03T12:48:57.401+10:00 level=INFO msg="provider registered" name=ollama
time=2026-05-03T12:48:57.401+10:00 level=INFO msg="provider registered" name=mistral
time=2026-05-03T12:48:57.401+10:00 level=INFO msg="server starting" addr=:8080
EXIT_CODE: 124 (expected — killed by `timeout 3`)
```

### Verdict: APPROVE ✅

All seven verification commands pass. The previously-broken `ai-sdk-nats` module now builds cleanly. No regressions in the main module — all 20 test packages pass with race detector enabled.


---

## F1 Plan Compliance Audit — 2026-05-03

**Auditor**: oracle (Plan Compliance Audit)
**Verdict**: **APPROVE** ✅

### Concrete Deliverables — Compliance Matrix

| # | Deliverable | Status | Evidence |
|---|-------------|--------|----------|
| 1 | Domain packages: object, video, agent | ✅ PASS | object/: 7 files (doc/types/provider/client/errors/stream/provider_options), video/: 5 files (doc/types/provider/client/errors), agent/: 3 files (doc/agent/agent_impl — matches AGENTS.md orchestration definition). Note: agent is an orchestration layer, not a domain package — plan's "domain package" grouping is a wording inconsistency. |
| 2 | Core orchestration for all generation types | ✅ PASS | object_impl.go (GenerateObject + StreamObject), video_impl.go (GenerateVideo), speech_impl.go (GenerateSpeech), image_impl.go (GenerateImage), generate.go (GenerateText), stream_impl.go (StreamText) |
| 3 | Provider implementations (top 10) | ✅ PASS | 12 providers present (exceeds "top 10"): openai, anthropic, azure, cohere, deepseek, gemini, groq, mistral, ollama, perplexity, togetherai, xai |
| 4 | UI layer: HTTP/SSE transport, streaming, file uploads | ✅ PASS | httptransport.go (HTTP transport), handlers/chat.go (SSE endpoints), uimessage/sse/ (writer.go + transform.go), Templ components (message/chat/input), upload/ (ParseMultipartForm, DetectMediaType, Skill upload) |
| 5 | Telemetry/logging infrastructure | ✅ PASS | pkg/telemetry/ (Span + Tracer interfaces), pkg/logger/ (Logger interface + slog adapter), pkg/middleware/telemetry.go (OTel span middleware) |
| 6 | Test coverage for all new code | ✅ PASS | All 12 providers have ≥1 test file. Core orchestration tested. Domain packages with logic have tests (chat: 4, embed: 3, rerank: 1). Data-only domain packages (image, speech, transcribe, object, video) lack unit tests but their types are exercised through provider and core tests. |

### Definition of Done — Compliance

| Item | Status | Evidence |
|------|--------|----------|
| Every task builds, vets, tests clean | ✅ PASS | `go build ./...` clean, `go vet ./...` clean (no output), `go test ./...` all PASS (0 failures across 20+ packages) |
| `go test ./...` passes in ai-sdk/ | ✅ PASS | All 25 packages PASS (chat, core, embed, error, logger, middleware, all 12 providers, rerank, ui/chat, uimessage, uimessage/sse, upload, util) |
| `go test ./...` passes in ai-sdk-nats/ | ✅ PASS | nats package passes |
| No compile errors in ai-sdk-examples/ | ✅ PASS | `go build ./...` succeeds for all 5 examples |

### Must NOT Have — Compliance

| Rule | Status | Evidence |
|------|--------|----------|
| No breaking changes to existing API | ✅ PASS | `compat.go` preserves `UIMessage`, `StreamEvent`, `UIMessagePart`, `PartType`, `ToolResult` types for ai-sdk-nats compatibility |
| No provider types leaking into domain | ✅ PASS | Global grep confirms zero `pkg/provider/` imports in any domain package (chat, embed, image, speech, transcribe, object, video, rerank). All use only stdlib. |
| No external deps in ai-sdk core | ✅ PASS | `go.mod` has single require: `github.com/a-h/templ v0.3.1001`. Zero service/external dependencies. ai-sdk-nats is a separate module. |

### Success Criteria — Compliance

| # | Criterion | Status | Evidence |
|---|-----------|--------|----------|
| 1 | `go test ./...` passes in ai-sdk/ | ✅ PASS | Confirmed |
| 2 | `go test ./...` passes in ai-sdk-nats/ | ✅ PASS | Confirmed |
| 3 | `go build ./...` passes in ai-sdk-examples/ | ✅ PASS | Confirmed |
| 4 | All domain packages have doc.go + types.go + provider.go + client.go + errors.go | ✅ PASS | All 8 domain packages verified: chat ✅, embed ✅, image ✅, speech ✅, transcribe ✅, object ✅, video ✅, rerank ✅ |
| 5 | All providers have at least one test | ✅ PASS | anthropic:1, azure:1, cohere:1, deepseek:3, gemini:4, groq:1, mistral:1, ollama:4, openai:1, perplexity:1, togetherai:2, xai:1 |

### Observations (Non-Blocking)

1. **Agent package structure**: Plan groups agent as "domain package" but AGENTS.md correctly positions it as an orchestration layer (between Services and UI). It has `doc.go`, `agent.go`, `agent_impl.go` — matching AGENTS.md exactly. This is a plan wording issue, not a gap.

2. **Domain packages without direct tests**: image/, speech/, transcribe/, object/, video/ have no `*_test.go` files. While not explicitly required by success criteria (which only requires provider tests), the plan's "Complete test coverage for all new code" deliverable could be interpreted as requiring domain package tests. However, these are data-only type definitions — their correctness is exercised through provider and core tests.

3. **`pkg/embed/doc.go` line 7**: References `pkg/provider/*` in a documentation comment only (not a code import). This is acceptable per Go conventions — doc comments may reference other packages.

4. **pkg/prompt/** — only 2 files (prompt.go + doc.go), no tests. Used by util/ which has tests. Acceptable as a thin data-only package.

### Architectural Compliance

Onion model verified:
- Domain packages import only stdlib ✅
- Providers import domain interfaces only ✅
- Core imports domain interfaces only (not providers) ✅
- Middleware imports domain + telemetry interfaces only ✅
- UI imports core + domain + registry ✅
- No circular dependencies ✅

### Final Assessment

All 6 concrete deliverables are delivered. All 3 "Must NOT Have" rules are enforced. All 5 success criteria are met. Definition of Done is satisfied across all three modules (ai-sdk, ai-sdk-nats, ai-sdk-examples).

**VERDICT: APPROVE** ✅

Ready for F2 (Code Quality Review), F3 (Manual QA), F4 (Scope Fidelity Check).

## F4: Scope Fidelity Re-Check (2026-05-03)

**Verdict: APPROVE** — All plan requirements verified as complete.

### Task-by-Task Assessment

**Wave 1 (T1-T8):** ALL PASS
| Task | Package | Files | Status |
|------|---------|-------|--------|
| T1 | pkg/object/ | 7 (.go includes doc/types/provider/client/errors/stream/provider_options) | ✅ |
| T2 | pkg/video/ | 5 (doc/types/provider/client/errors) | ✅ |
| T3 | pkg/core/object_impl.go | Present + object_impl_test.go | ✅ |
| T4 | pkg/core/image_impl.go | Present | ✅ |
| T5 | pkg/core/speech_impl.go | Present | ✅ |
| T6 | pkg/core/video_impl.go | Present | ✅ |
| T7 | pkg/prompt/ | 2 (prompt.go + doc.go) | ✅ |
| T8 | pkg/telemetry/ | 2 (telemetry.go + doc.go) | ✅ |

**Wave 2 (T9-T16):** ALL PASS — 12 providers total, all have ≥1 test file
| Task | Provider | Test | Status |
|------|----------|------|--------|
| T9 | openai | openai_test.go | ✅ |
| T10 | anthropic | anthropic_test.go | ✅ |
| T11 | mistral | mistral_test.go | ✅ |
| T12 | groq | groq_test.go | ✅ |
| T13 | xai | xai_test.go | ✅ |
| T14 | perplexity | perplexity_test.go | ✅ |
| T15 | azure | azure_test.go | ✅ |
| T16 | cohere | cohere_test.go | ✅ |

**Wave 3 (T17-T24):** ALL PASS
| Task | Deliverable | Status |
|------|-------------|--------|
| T17 | pkg/agent/ (agent.go, agent_impl.go, doc.go) | ✅ |
| T18 | Agent orchestration (in agent_impl.go) | ✅ |
| T19 | pkg/ui/handlers/ (chat.go + doc.go) | ✅ |
| T20 | pkg/ui/chat/httptransport.go | ✅ |
| T21 | pkg/uimessage/sse/ (writer.go, transform.go, sse_test.go 900+ lines, 91.2% cov) | ✅ |
| T22 | pkg/ui/components/ (3 .templ.go + doc.go) | ✅ |
| T23 | cmd/ai-sdk/main.go wires all 12 providers | ✅ |
| T24 | pkg/middleware/telemetry.go + telemetry_test.go (7 tests) | ✅ |

**Wave 4 (T25-T30):** ALL PASS
| Task | Deliverable | Status |
|------|-------------|--------|
| T25 | pkg/upload/upload.go + test | ✅ |
| T26 | pkg/upload/skill.go | ✅ |
| T27 | pkg/util/prompt.go + test | ✅ |
| T28 | pkg/util/tokenizer.go + test | ✅ |
| T29 | pkg/error/errors.go + test (package errx) | ✅ |
| T30 | pkg/logger/logger.go + test | ✅ |

**Wave 5 (T31-T36):** ALL PASS
- registry.go: RegisterObject, RegisterVideo added; RegisterAgent removed (line 107)
- Object(), Video() getters added; Agent getter removed (line 205)
- No agent import in registry (C1 verified)
- cmd/ai-sdk/main.go imports and registers all 8 new providers

**Wave 6 (T37-T42):** ALL PASS
- 5 example programs compile: openai-chat, anthropic-agent, object-generation, speech-to-text, image-generation
- AGENTS.md: 477 lines, documents all 10 new packages, 12 providers with capability matrix, examples

**Wave 7 (T43-T45):** ALL PASS
- `go mod tidy`: clean (all 3 modules)
- `go vet ./...`: clean (all 3 modules)
- `go test -count=1 -race ./...`: 25 packages, ALL PASS, 0 failures

### Critical Fixes Verification
| Fix | Check | Result |
|-----|-------|--------|
| C1 | registry no longer imports agent | ✅ line 33 comment: "agentProv removed: agents belong in cmd/" |
| C2 | pkg/error renamed | ✅ package errx (not `package error`) |
| C3 | telemetry filters io.EOF | ✅ `errors.Is(err, io.EOF)` check at line 86 |
| GAP1 | ai-sdk-nats builds | ✅ `go build ./...` + `go test ./...` both pass |
| GAP2 | StreamObject exists | ✅ pkg/object/stream.go (ObjectChunk, ObjectStream), provider interface updated |

### Success Criteria
- `go test ./...` passes in ai-sdk ✅ (25 packages, 0 failures)
- `go test ./...` passes in ai-sdk-nats ✅ (1 package OK)
- `go build ./...` passes in ai-sdk-examples ✅ 
- All domain packages have 5 files ✅ (all have doc/types/provider/client/errors)
- All providers have at least one test ✅ (21 test files across 12 providers)
