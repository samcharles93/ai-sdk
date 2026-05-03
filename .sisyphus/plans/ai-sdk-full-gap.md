# AI SDK Go — Full Gap Plan

## TL;DR

> **Reference**: Vercel AI SDK (`packages/ai/src/`) at `/work/clones/ai`
> **Current**: Go re-interpretation at `/work/projects/ai-sdk`
> **Scope**: Map every missing capability from the JS SDK into our Go SDK
>
> **Deliverables**: `.sisyphus/plans/ai-sdk-full-gap.md` (this file)
> **Estimated Effort**: XL (50+ tasks across 7 waves)
> **Parallel Execution**: YES — 7 waves, max 8 tasks per wave

---

## Context

### Reference SDK Capabilities (Vercel AI SDK)

| Module | Description | Status |
|---|---|---|
| `generate-text` | Text generation with tools | ✅ Ported |
| `stream-text` | Streaming text generation | ✅ Ported |
| `generate-object` | Structured JSON object generation | ❌ Missing |
| `stream-object` | Streaming structured objects | ❌ Missing |
| `generate-image` | Image generation | 🟡 Domain types exist, no orchestration |
| `generate-speech` | Text-to-speech | 🟡 Domain types exist, no orchestration |
| `generate-video` | Video generation | ❌ Missing entirely |
| `transcribe` | Audio transcription | 🟡 Domain types exist, no orchestration |
| `embed` | Text embeddings | ✅ Ported |
| `rerank` | Document reranking | ✅ Ported (TogetherAI) |
| `agent` | Tool-loop agent with UI stream | ❌ Missing entirely |
| `ui` | `useChat`, `Chat`, transports | 🟡 Partial (Chat struct, no HTTP transport) |
| `ui-message-stream` | SSE streaming of UI messages | ❌ Missing |
| `registry` | Model registry | 🟡 Exists, missing providers |
| `middleware` | Provider middleware | 🟡 Exists, under-utilized |
| `telemetry` | OpenTelemetry instrumentation | ❌ Missing |
| `upload-file` | File upload handling | ❌ Missing |
| `upload-skill` | Skill/template upload | ❌ Missing |
| `error` | Error types | 🟡 Partial |
| `logger` | Logging utilities | 🟡 Partial |
| `prompt` | Prompt engineering utilities | ❌ Missing |
| `util` | Shared utilities | 🟡 Partial |

### Provider Ecosystem

| Provider | Reference | Our SDK | Status |
|---|---|---|---|
| openai | ✅ | ❌ | Missing |
| anthropic | ✅ | ❌ | Missing |
| google-vertex | ✅ | ❌ | Missing |
| google (Gemini) | ✅ | ✅ | Done |
| deepseek | ✅ | ✅ | Done |
| ollama | ✅ | ✅ | Done |
| togetherai | ✅ | ✅ | Done |
| mistral | ✅ | ❌ | Missing |
| groq | ✅ | ❌ | Missing |
| perplexity | ✅ | ❌ | Missing |
| xai | ✅ | ❌ | Missing |
| cohere | ✅ | ❌ | Missing |
| amazon-bedrock | ✅ | ❌ | Missing |
| azure | ✅ | ❌ | Missing |
| fireworks | ✅ | ❌ | Missing |
| voyage | ✅ | ❌ | Missing |
| fal | ✅ | ❌ | Missing |
| luma | ✅ | ❌ | Missing |
| elevenlabs | ✅ | ❌ | Missing |
| assemblyai | ✅ | ❌ | Missing |
| deepgram | ✅ | ❌ | Missing |
| revai | ✅ | ❌ | Missing |
| lmnt | ✅ | ❌ | Missing |
| hume | ✅ | ❌ | Missing |
| gladia | ✅ | ❌ | Missing |
| klingai | ✅ | ❌ | Missing |
| baseten | ✅ | ❌ | Missing |
| black-forest-labs | ✅ | ❌ | Missing |
| bytedance | ✅ | ❌ | Missing |
| deepinfra | ✅ | ❌ | Missing |
| cerebras | ✅ | ❌ | Missing |
| prodia | ✅ | ❌ | Missing |
| replicate | ✅ | ❌ | Missing |
| alibaba | ✅ | ❌ | Missing |
| mcp | ✅ | ❌ | Missing |

### UI Layer

| JS Concept | Go Equivalent | Status |
|---|---|---|
| `useChat()` hook | `chat.Chat` struct | 🟡 Exists, needs HTTP transport |
| `UIMessage` | `chat.UIMessage` | ✅ Exists |
| `ChatTransport` | `chat.Transport` interface | 🟡 Exists, only DirectTransport |
| `sendMessage()` | `Chat.Send()` | 🟡 Exists |
| `addToolOutput()` | `Chat.AddToolOutput()` | 🟡 Exists |
| `onToolCall` | `ChatOptions.OnToolCall` | 🟡 Exists |
| `onFinish` | `ChatOptions.OnFinish` | 🟡 Exists |
| `http-chat-transport` | `NatsTransport` in ai-sdk-nats | 🟡 Exists in separate module |
| SSE streaming | `uimessage/sse` package | 🟡 Exists but minimal |
| Templ components | `.templ` files in `pkg/ui/components/` | 🟡 Skeleton exists |
| Datastar reactivity | `data-star` attributes | 🟡 Referenced, not wired |
| File upload | `convert-file-list` | ❌ Missing |
| `useCompletion` | No equivalent | ❌ Missing |
| `text-stream-chat-transport` | No equivalent | ❌ Missing |
| `process-ui-message-stream` | No equivalent | ❌ Missing |

---

## Work Objectives

### Core Objective
Port every missing capability from the Vercel AI SDK to the Go re-interpretation, prioritizing by value to a chat app built on the SDK.

### Concrete Deliverables
- Domain packages for every missing capability (object generation, video, agent)
- Orchestration in `pkg/core/` for every missing generation type
- Provider implementations for the top 10 providers
- UI layer completion: HTTP/SSE transport, real-time streaming, file uploads
- Telemetry/logging infrastructure
- Complete test coverage for all new code

### Definition of Done
- Every task builds, vets, and tests clean
- `go test ./...` passes in `ai-sdk/` and `ai-sdk-nats/`
- No compile errors in `ai-sdk-examples/`

### Must NOT Have
- No breaking changes to existing API surface
- No provider-specific types leaking into domain packages
- No external dependencies in `ai-sdk` core module (only `ai-sdk-nats` adds NATS)

---

## Verification Strategy

### Test Decision
- **Infrastructure exists**: YES (`go test` works)
- **Automated tests**: TDD for new domain packages + orchestration
- **Framework**: `go test`
- **Agent-Executed QA**: Every task includes explicit verification commands

### QA Policy
Every task MUST include agent-executed QA scenarios (see TODO template).

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Foundation — can all start immediately):
├── T1.  pkg/object/ domain package (types, provider, client, errors)
├── T2.  pkg/video/ domain package (types, provider, client, errors)
├── T3.  pkg/core/object_impl.go — GenerateObject + StreamObject orchestration
├── T4.  pkg/core/image_impl.go — GenerateImage orchestration
├── T5.  pkg/core/speech_impl.go — GenerateSpeech orchestration
├── T6.  pkg/core/video_impl.go — GenerateVideo orchestration
├── T7.  pkg/prompt/ — Prompt engineering utilities (system prompts, templates)
└── T8.  pkg/telemetry/ — OpenTelemetry span types

Wave 2 (Provider Expansion — depends: domain packages from Wave 1):
├── T9.  pkg/provider/openai/ — Chat + completion provider
├── T10. pkg/provider/anthropic/ — Claude provider
├── T11. pkg/provider/mistral/ — Mistral provider
├── T12. pkg/provider/groq/ — Groq provider
├── T13. pkg/provider/xai/ — xAI/Grok provider
├── T14. pkg/provider/perplexity/ — Perplexity provider
├── T15. pkg/provider/azure/ — Azure OpenAI provider
├── T16. pkg/provider/cohere/ — Cohere provider

Wave 3 (Agent + Advanced Features):
├── T17. pkg/agent/ — Tool-loop agent with UI stream
├── T18. pkg/core/agent_impl.go — Agent orchestration
├── T19. pkg/ui/handlers/ — HTTP handlers for chat + SSE
├── T20. pkg/ui/chat/httpttransport.go — HTTP transport implementing Transport
├── T21. pkg/uimessage/sse/ — Complete SSE message streaming
├── T22. pkg/ui/components/ — Wire Templ components with Datastar
├── T23. cmd/ai-sdk/ — Full entrypoint with all providers wired
└── T24. pkg/middleware/telemetry.go — OTel span middleware

Wave 4 (File + Upload + Utility):
├── T25. pkg/upload/ — File upload handling (multipart, base64)
├── T26. pkg/upload/skill.go — Skill/template upload
├── T27. pkg/util/prompt.go — Prompt engineering helpers
├── T28. pkg/util/tokenizer.go — Token counting utilities
├── T29. pkg/error/ — Sentinel errors expansion
└── T30. pkg/logger/ — Structured logging utilities

Wave 5 (Registry + Wiring):
├── T31. pkg/registry/ — Register all new providers
├── T32. pkg/registry/rerank.go — Already done, verify completeness
├── T33. pkg/registry/object.go — Register object providers
├── T34. pkg/registry/video.go — Register video providers
├── T35. pkg/registry/agent.go — Register agent providers
└── T36. cmd/ai-sdk/ — Update entrypoint with new registry calls

Wave 6 (Examples + Docs):
├── T37. ai-sdk-examples/openai-chat/ — OpenAI chat example
├── T38. ai-sdk-examples/anthropic-agent/ — Agent example
├── T39. ai-sdk-examples/object-generation/ — GenerateObject example
├── T40. ai-sdk-examples/speech-to-text/ — Transcribe example
├── T41. ai-sdk-examples/image-generation/ — Image generation example
└── T42. AGENTS.md update — Document all new packages

Wave 7 (Polish + Final Review):
├── T43. go mod tidy across all modules
├── T44. go vet ./... across all modules
├── T45. go test ./... across all modules
├── F1.  Plan compliance audit (oracle)
├── F2.  Code quality review (unspecified-high)
├── F3.  Real manual QA (unspecified-high)
└── F4.  Scope fidelity check (deep)
```

### Critical Path
T1 → T3 → T17 → T18 → T19 → T23 → F1-F4 → user okay

---

## TODOs

- [x] T1. **pkg/object/ domain package** — types, provider interface, client, errors

  **What to do**: Scaffold `pkg/object/` following the onion model: `doc.go`, `types.go` (Request/Response/ObjectResult), `provider.go` (ObjectProvider interface), `client.go` (nil-guard facade), `errors.go` (sentinel errors). Match existing `pkg/chat/` pattern exactly.

  **Must NOT do**: No provider implementations, no orchestration logic.

  **Recommended Agent Profile**: `quick` — scaffolding task

  **Parallelization**: Wave 1 (with T2-T8)

  **Acceptance Criteria**:
  - `go build ./pkg/object/` passes
  - All 5 files (`doc.go`, `types.go`, `provider.go`, `client.go`, `errors.go`) exist and compile
  - `ObjectProvider` interface has `GenerateObject(ctx, req) (ObjectResult, error)` and `StreamObject(ctx, req) (ObjectStream, error)` methods

  **QA Scenarios**:
  ```
  Scenario: Build check
    Tool: Bash
    Preconditions: ai-sdk repo at /work/projects/ai-sdk
    Steps:
      1. go build ./pkg/object/
    Expected Result: No output (success)
    Evidence: .sisyphus/evidence/t1-build.txt
  ```

  **Commit**: YES (Wave 1)

- [x] T2. **pkg/video/ domain package** — types, provider interface, client, errors

  **What to do**: Scaffold `pkg/video/` following `pkg/image/` pattern. Types for video generation requests/responses.

  **Must NOT do**: No provider implementations.

  **Recommended Agent Profile**: `quick`

  **Parallelization**: Wave 1

  **Acceptance Criteria**:
  - `go build ./pkg/video/` passes
  - `VideoProvider` interface with `GenerateVideo(ctx, req) (VideoResult, error)`

  **Commit**: YES (Wave 1)

- [x] T3. **pkg/core/object_impl.go** — GenerateObject + StreamObject orchestration

  **What to do**: Implement `GenerateObject` and `StreamObject` in `pkg/core/`. Follow `generateText`/`streamText` pattern: call provider, validate JSON schema, parse result, return structured object.

  **Must NOT do**: No schema validation library (use existing `pkg/schema/`).

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 1 (depends: T1)

  **Acceptance Criteria**:
  - `go test ./pkg/core/...` passes
  - `GenerateObject` returns typed struct from provider JSON
  - `StreamObject` emits partial objects via channel

  **Commit**: YES (Wave 1)

- [x] T4. **pkg/core/image_impl.go** — GenerateImage orchestration

  **What to do**: Implement `GenerateImage` orchestration in `pkg/core/`. Follow `generateText` pattern. Types already exist in `pkg/image/`.

  **Must NOT do**: No image encoding logic (providers return URLs/data).

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 1 (depends: `pkg/image/` exists)

  **Acceptance Criteria**:
  - `go build ./pkg/core/` passes
  - `GenerateImage` calls provider, returns `image.GenerateImageResponse`

  **Commit**: YES (Wave 1)

- [x] T5. **pkg/core/speech_impl.go** — GenerateSpeech orchestration

  **What to do**: Implement `GenerateSpeech` in `pkg/core/`. Types exist in `pkg/speech/`.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 1 (depends: `pkg/speech/` exists)

  **Commit**: YES (Wave 1)

- [x] T6. **pkg/core/video_impl.go** — GenerateVideo orchestration

  **What to do**: Implement `GenerateVideo` in `pkg/core/`. Types from T2.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 1 (depends: T2)

  **Commit**: YES (Wave 1)

- [x] T7. **pkg/prompt/ — Prompt engineering utilities**

  **What to do**: Create `pkg/prompt/` with helpers for system prompts, user prompts, message formatting. Follow reference SDK's `prompt/` module.

  **Recommended Agent Profile**: `quick`

  **Parallelization**: Wave 1

  **Commit**: YES (Wave 1)

- [x] T8. **pkg/telemetry/ — OpenTelemetry span types**

  **What to do**: Create `pkg/telemetry/` with span interfaces for tracing LLM calls. No OTel dependency in core — just types. OTel integration goes in middleware.

  **Recommended Agent Profile**: `quick`

  **Parallelization**: Wave 1

  **Commit**: YES (Wave 1)

- [x] T9. **pkg/provider/openai/ — Chat + completion provider**

  **What to do**: Implement OpenAI provider for `chat.Provider`, `embed.Provider`, `image.Provider`, `speech.Provider`. Follow `pkg/provider/deepseek/` pattern.

  **Must NOT do**: No provider-specific types in domain packages.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 2 (depends: all domain packages stable)

  **Acceptance Criteria**:
  - `go test ./pkg/provider/openai/...` passes
  - Implements chat, embed, image, speech interfaces
  - HTTP mocking with `httptest.Server`

  **Commit**: YES (Wave 2)

- [x] T10. **pkg/provider/anthropic/ — Claude provider**

  **What to do**: Implement Anthropic provider for `chat.Provider`. Supports messages API, tool use, streaming.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 2

  **Commit**: YES (Wave 2)

- [x] T11. **pkg/provider/mistral/ — Mistral provider**

  **What to do**: Implement Mistral provider for `chat.Provider`, `embed.Provider`.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 2

  **Commit**: YES (Wave 2)

- [x] T12. **pkg/provider/groq/ — Groq provider**

  **What to do**: Implement Groq provider for `chat.Provider` (OpenAI-compatible API).

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 2

  **Commit**: YES (Wave 2)

- [x] T13. **pkg/provider/xai/ — xAI/Grok provider**

  **What to do**: Implement xAI provider for `chat.Provider`.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 2

  **Commit**: YES (Wave 2)

- [x] T14. **pkg/provider/perplexity/ — Perplexity provider**

  **What to do**: Implement Perplexity provider for `chat.Provider`.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 2

  **Commit**: YES (Wave 2)

- [x] T15. **pkg/provider/azure/ — Azure OpenAI provider**

  **What to do**: Implement Azure OpenAI provider for `chat.Provider`, `embed.Provider`, `image.Provider`.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 2

  **Commit**: YES (Wave 2)

- [x] T16. **pkg/provider/cohere/ — Cohere provider**

  **What to do**: Implement Cohere provider for `chat.Provider`, `embed.Provider`, `rerank.Provider`.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 2

  **Commit**: YES (Wave 2)

- [x] T17. **pkg/agent/ — Tool-loop agent with UI stream**

  **What to do**: Create `pkg/agent/` with `Agent` struct that orchestrates multi-step tool loops. Follow `ToolLoopAgent` pattern from reference SDK.

  **Must NOT do**: No UI rendering — just the agent orchestration.

  **Recommended Agent Profile**: `deep`

  **Parallelization**: Wave 3 (depends: T1-T8, all providers)

  **Acceptance Criteria**:
  - `go test ./pkg/agent/...` passes
  - Agent can run tool-call loop with mock provider
  - UI stream events emitted correctly

  **Commit**: YES (Wave 3)

- [x] T18. **pkg/core/agent_impl.go — Agent orchestration** (implemented in `pkg/agent/agent_impl.go` due to circular dependency avoidance)

  **What to do**: Implement agent-specific orchestration in `pkg/core/` that bridges `pkg/agent/` with providers.

  **Recommended Agent Profile**: `deep`

  **Parallelization**: Wave 3 (depends: T17)

  **Commit**: YES (Wave 3)

- [x] T19. **pkg/ui/handlers/ — HTTP handlers for chat + SSE**

  **What to do**: Implement real HTTP handlers in `pkg/ui/handlers/`:
  - POST /chat/send — accepts user message, starts Bridge
  - GET /chat/stream — SSE endpoint that subscribes to NATS and streams events
  - Both wired with real provider registry

  **Recommended Agent Profile**: `visual-engineering`

  **Parallelization**: Wave 3 (depends: all prior)

  **Acceptance Criteria**:
  - Handler responds with correct SSE format
  - Events flow end-to-end (HTTP → Bridge → NATS → Transport → SSE)
  - `curl` test verifies SSE output

  **Commit**: YES (Wave 3)

- [x] T20. **pkg/ui/chat/httptransport.go — HTTP transport**

  **What to do**: Implement `HTTPTransport` that implements `chat.Transport` via HTTP POST + SSE. For browser clients that can't use NATS directly.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 3

  **Commit**: YES (Wave 3)

- [x] T21. **pkg/uimessage/sse/ — Complete SSE message streaming**

  **What to do**: Expand `pkg/uimessage/sse/` to handle all `StreamEvent` types (text, tool-call, tool-result, reasoning, step-start, finish, error).

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 3

  **Commit**: YES (Wave 3)

- [x] T22. **pkg/ui/components/ — Wire Templ components**

  **What to do**: Make `.templ` components actually render events from SSE. Add Datastar `data-on-*` handlers for real-time updates.

  **Recommended Agent Profile**: `visual-engineering`

  **Parallelization**: Wave 3

  **Commit**: YES (Wave 3)

- [x] T23. **cmd/ai-sdk/ — Full entrypoint**

  **What to do**: Complete `cmd/ai-sdk/` to wire all providers, start embedded NATS, serve HTTP with handlers, and run a working chat app.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 3 (depends: all prior)

  **Commit**: YES (Wave 3)

- [x] T24. **pkg/middleware/telemetry.go — OTel middleware**

  **What to do**: Add telemetry middleware that wraps providers and emits OpenTelemetry spans.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 3

  **Commit**: YES (Wave 3)

- [x] T25. **pkg/upload/ — File upload handling**

  **What to do**: Create `pkg/upload/` for multipart file upload, base64 encoding, media type detection.

  **Recommended Agent Profile**: `quick`

  **Parallelization**: Wave 4

  **Commit**: YES (Wave 4)

- [x] T26. **pkg/upload/skill.go — Skill/template upload**

  **What to do**: Upload skill definitions (system prompt templates).

  **Recommended Agent Profile**: `quick`

  **Parallelization**: Wave 4

  **Commit**: YES (Wave 4)

- [x] T27-T30. **Utility packages**

  **What to do**: Complete `pkg/util/prompt.go` (prompt engineering), `pkg/util/tokenizer.go` (token counting), `pkg/error/` (expanded errors), `pkg/logger/` (structured logging).

  **Recommended Agent Profile**: `quick`

  **Parallelization**: Wave 4

  **Commit**: YES (Wave 4)

- [x] T31-T36. **Registry wiring**

  **What to do**: Register all new providers in `pkg/registry/`. Add `RegisterObject`, `RegisterVideo`, `RegisterAgent` helpers.

  **Recommended Agent Profile**: `quick`

  **Parallelization**: Wave 5

  **Commit**: YES (Wave 5)

- [x] T37-T42. **Examples + docs**

  **What to do**: Create examples in `ai-sdk-examples/` for OpenAI chat, agent, object generation, speech-to-text, image generation. Update `AGENTS.md`.

  **Recommended Agent Profile**: `unspecified-high`

  **Parallelization**: Wave 6

  **Commit**: YES (Wave 6)

- [ ] T43-T45. **Final verification**

  **What to do**: `go mod tidy`, `go vet ./...`, `go test ./...` across all modules.

  **Recommended Agent Profile**: `quick`

  **Parallelization**: Wave 7

  **Commit**: YES (Wave 7)

---

## Final Verification Wave

- [ ] F1. **Plan Compliance Audit** — `oracle`
- [ ] F2. **Code Quality Review** — `unspecified-high`
- [ ] F3. **Real Manual QA** — `unspecified-high`
- [ ] F4. **Scope Fidelity Check** — `deep`

---

## Commit Strategy

- Each wave commits as a group
- Message: `feat(pkg/object): add domain types and provider interface`
- Pre-commit: `go test ./pkg/object/...`

---

## Success Criteria

- `go test ./...` passes in `ai-sdk/`
- `go test ./...` passes in `ai-sdk-nats/`
- `go build ./...` passes in `ai-sdk-examples/`
- All new domain packages have `doc.go` + `types.go` + `provider.go` + `client.go` + `errors.go`
- All new providers have at least one test
