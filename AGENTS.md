# AGENTS.md — AI SDK

Guidance for any coding agent working in this repository.

Keep every rule here harness-agnostic: name the required behaviour, not the tool
that provides it (e.g. "spawn a fresh reviewer that did not write the code", not
a specific sub-command).

## Code Quality & Refactoring Standards

- **Zero Patch Stacking:** Never apply more than two sequential fixes to the
  same logic block. If a solution fails twice, scrap the block and rewrite it
  cleanly.
- **Root-Cause Fixes:** Fix invalid state at the producer, not via defensive
  checks at the consumer.
- **Architectural Simplicity:** Prefer a complete 20-line rewrite over a 5-line
  band-aid that adds conditional complexity or obscures intent.
- **Revert on Flail:** If an implementation becomes convoluted to satisfy edge
  cases, discard the approach and select a simpler design.

## What this is

**ai-sdk** is a provider-agnostic Go SDK that unifies eight domains — chat, embedding, image
generation (and image editing), speech synthesis, transcription, object
(structure) generation, video generation, and reranking — behind typed domain
interfaces (`chat.Provider`, `image.Provider`, `video.Provider`, ...). A pluggable
`runtime` resolves `provider/model` references into working providers so
applications (such as `archied`) can consume AI backends without hardcoding any
implementation.

The architecture sections below are authoritative for settled design. Read them
before making non-trivial changes. `cmd/` is the composition root — it wires
domains to concrete providers and starts the server.

## Scope Discipline

The architecture + package reference sections are the settled design.

- **Settled design exists** → Implement it immediately with a diff. Do not
  produce design prose. If a small detail is missing, ask the maintainer
  directly.
- **No settled design exists** → Write a decisive, 1-page design doc before
  coding, or open a decision in beads.
- **Strictly Prohibited:** Ownership ledgers, field-level inventories,
  current-state traces, parity matrices for their own sake, and
  planning/refactor tracking issues that duplicate beads.
- **Solo Project Context:** Prefer the smallest workable change over extensive
  defensive scaffolding.

## Architecture (Onion Model) — CORE | STRICT

This project follows the **onion model** (also known as hexagonal / ports &
adapters). Each layer is responsible for a specific concern and **MUST NOT**
know about any layer above it. Dependency direction: **inward only** — outer
layers depend on inner layers, never the reverse.

```tree
┌──────────────────────────────────────────────────┐
│  UI Layer        ui/                         │  Templ + Datastar components & handlers
│  ─────────────────────────────────────────────── │  Knows: services, domain interfaces
│                  uimessage/                   │  UI message protocol (SSE, chunks)
│                  uimessage/sse/               │  SSE writer, stream processor
├──────────────────────────────────────────────────┤
│  Runtime         runtime/                     │  Provider-agnostic model resolution:
│  ─────────────────────────────────────────────── │    models.dev catalog, pluggable
│                                                   │    ProviderClass registry, Chat/Embed
│                                                   │    entry points.
│                                                   │  Knows: catalog, classes, core, domains
├──────────────────────────────────────────────────┤
│  Agent           agent/                       │  Tool-loop agent over StreamText
│  ─────────────────────────────────────────────── │  Knows: core, domain interfaces
├──────────────────────────────────────────────────┤
│  Services        core/                       │  Orchestration: GenerateText, StreamText,
│  ─────────────────────────────────────────────── │    GenerateObject, GenerateImage,
│                  chat/client.go               │    GenerateSpeech
│                  embed/client.go              │  Knows: domain interfaces (Provider)
│                  image/client.go              │  Thin facades over providers
│                  speech/client.go             │
│                  transcribe/client.go         │
│                  object/client.go             │
│                  video/client.go              │
│                  rerank/client.go             │
├──────────────────────────────────────────────────┤
│  Middleware      middleware/                  │  Wraps domain interfaces
│  ─────────────────────────────────────────────── │  Knows: domain, telemetry interfaces
├──────────────────────────────────────────────────┤
│  Infrastructure  registry/                    │  Provider registry
│  ─────────────────────────────────────────────── │  Knows: domain interfaces
│                  schema/                      │  JSON Schema builder (standalone)
│                  util/                        │  Prompt helpers, tokenizer (stdlib only)
│                  upload/                      │  Multipart form parsing
│                  error/                       │  Sentinel errors (stdlib only)
│                  logger/                      │  Structured logging abstraction
│                  telemetry/                   │  OTel-compatible tracing interfaces
│                  prompt/                      │  Prompt manager (standalone)
├──────────────────────────────────────────────────┤
│  Domain          chat/                        │  Chat types + Provider interface
│  Interfaces      embed/                       │  Embedding types + Provider
│  ─────────────── image/                       │  Image gen types + Provider (+ Editor)
│  (INNERMOST)     speech/                      │  Speech synthesis types + Provider
│                  transcribe/                   │  Transcription types + Provider
│                  object/                      │  Object gen types + Provider
│                  video/                       │  Video gen types + Provider
│                  rerank/                      │  Reranking types + Provider
│                                                  │  Knows: NOTHING (stdlib only)
├──────────────────────────────────────────────────┤
│  Providers       provider/anthropic/          │  Implements domain interfaces
│  ─────────────── provider/azure/              │  Knows: domain interfaces + HTTP APIs
│                  provider/cohere/              │
│                  provider/deepseek/            │
│                  provider/gemini/              │
│                  provider/groq/                │
│                  provider/mistral/             │
│                  provider/ollama/              │
│                  provider/openai/              │
│                  provider/openaiobject/        │
│                  provider/perplexity/          │
│                  provider/togetherai/          │
│                  provider/xai/                 │
└──────────────────────────────────────────────────┘
```

### Dependency Rules (NON-NEGOTIABLE)

1. **Domain packages (`chat`, `embed`, etc.)** MUST NOT import any other
   package. Only stdlib imports are allowed.
   - The required `client.go` in each domain package is a thin in-package facade
     over that package's own `Provider` interface and types. It remains
     domain-layer code and does not import `core` or any other package.
2. **Provider packages (`provider/*`)** MAY import domain packages
   (`chat`, `embed`) to implement their interfaces. They MUST NOT import
   `core`, `ui`, `registry`, or `middleware`. A provider package MUST NOT import
   another provider package.
3. **Core/Services (`core/`)** MAY import domain packages and their
   interfaces. It MUST NOT import provider implementations or UI packages. It
   works strictly against interfaces.
   - The `client.go` files shown in the Services row (for example,
     `chat/client.go`) live in their respective domain packages. `core/`
     does not own these files; it imports domain packages.
4. **Runtime (`runtime/`)** MAY import domain packages, provider
   implementations, and core. It is the provider-resolution and model-discovery
   layer. It MUST NOT be imported by providers, domain packages, or core.
5. **Middleware (`middleware/`)** MAY import domain packages and
   infrastructure packages (`telemetry/`, `logger/`, `error/`). It
   MUST NOT import `core/`, provider implementations, `ui/`, or
   `runtime/`.
6. **Agent (`agent/`)** MAY import `core/` and domain packages. It MUST
   NOT import `ui/`, `runtime/`, or provider implementations.
7. **Infrastructure (`registry/`, `schema/`, `util/`, `upload/`,
   `error/`, `logger/`, `telemetry/`, `prompt/`)**:
   - `registry` — MAY import all domain interface packages. MUST NOT import
     providers, core, or UI.
   - `schema` — standalone, no package imports.
   - `util` — standalone, stdlib only.
   - `upload` — MAY import stdlib and domain types where needed for
     transport-level file handling. MUST NOT import providers, core, runtime, or
     UI.
   - `error` — standalone sentinel errors; stdlib only.
   - `logger` — standalone logging abstraction over stdlib logging
     primitives/interfaces. MUST NOT import providers, core, runtime, or UI.
   - `telemetry` — standalone tracing interfaces and no-op implementations. MUST
     NOT import providers, core, runtime, or UI.
   - `prompt` — standalone prompt management utilities; MUST NOT import
     providers, core, runtime, or UI.
8. **UI (`ui/`)** is the outermost layer. It MAY import core, domain
   interfaces, registry, and runtime. It MUST NOT import provider
   implementations directly. It contains:
   - State management structs (Go equivalents of React hooks like `useChat`)
   - Templ components (`.templ` files)
   - HTTP handlers
   - All UI depends on Datastar for streaming reactivity.
9. **`cmd/`** is the composition root. It wires everything together via
   dependency injection. It MAY import all packages.

### Package Conventions

Every domain package MUST contain:

- `doc.go` — Package-level documentation
- `types.go` — Request/Response types
- `provider.go` — Provider interface
- `client.go` — Thin Client facade with nil-guard
- `errors.go` — Sentinel errors

### Interface Ownership

Following
[Go's interface conventions](https://go.dev/wiki/CodeReviewComments#interfaces):

- **Consumers define interfaces they need**, not producers.
- Domain packages define the `Provider` interface because they are consumed by
  higher layers.
- HTTP handlers define service interfaces, not the other way around.
- **Optional capabilities** are separate interfaces (e.g. `image.Editor`, an
  optional image-edit capability) rather than methods bolted onto the base
  `Provider`. Callers type-assert before use; keep the base `Provider` minimal so
  every implementer builds without adding every optional method.

### Dependency Injection

- Every struct has a `New` constructor accepting its dependencies as interfaces.
- No global state. No package-level singletons. No `init()` for wiring.
- Changing a `New` signature produces compile-time errors showing all affected
  call sites.

## Runtime Capabilities

The `runtime` package is the provider-resolution layer. `ProviderSet` carries
one optional field per domain (Chat, Embed, Image, Video, Object, Rerank,
Speech, Transcribe); `Capability` constants name each; `Supports`/`Has` report
what a class/provider can satisfy. Built-in classes are registered by
`runtime.RegisterBuiltinClasses()`.

`Runtime` exposes a high-level entry point per domain, mirroring the
`ChatProvider`/`Chat` pattern:

```go
rt.Image(ctx, "xai/grok-image", image.GenerateImageRequest{...})      // image.GenerateImageResponse
rt.Video(ctx, "xai/grok-video", video.GenerateVideoRequest{...})      // video.GenerateVideoResponse
rt.Object(ctx, "openaiobject/gpt-4o-mini", object.Request{...})       // object.ObjectResult
rt.Rerank(ctx, "cohere/rerank-v3.5", rerank.Request{...})             // rerank.Response
rt.Speech(ctx, "openai/tts-1", speech.GenerateSpeechRequest{...})     // speech.GenerateSpeechResponse
rt.Transcribe(ctx, "openai/whisper-1", transcribe.TranscribeRequest{...})
```

Each domain also exposes a `*Provider` resolver
(`rt.ImageProvider(ctx, ref)` returns `(image.Provider, modelID, error)`).

**Extensibility:**

```go
runtime.RegisterClass(myCustomClass{})
```

A custom `ProviderClass` does arbitrary setup (discovery, auth exchange, header
injection) before returning a `ProviderSet`. This is the escape hatch for
providers not covered by the built-in classes.

## Provider Ecosystem

Capabilities as resolvable through the runtime (the `Class` column is the
`ProviderConfig.Class` value):

| Provider      | Package                      | Class          | Chat | Embed | Image | Video | Object | Rerank | Speech | Transcribe |
| ------------- | ---------------------------- | -------------- | ---- | ----- | ----- | ----- | ------ | ------ | ------ | ---------- |
| OpenAI        | `provider/openai`            | `openai`       | ✅   | —     | —     | —     | —      | —      | ✅     | ✅         |
| OpenAIObject  | `provider/openaiobject`      | `openaiobject` | —    | —     | —     | —     | ✅     | —      | —      | —          |
| Anthropic     | `provider/anthropic`         | `anthropic`    | ✅   | —     | —     | —     | —      | —      | —      | —          |
| Azure         | `provider/azure`             | `azure`        | ✅   | ✅    | ✅    | —     | —      | —      | —      | —          |
| Cohere        | `provider/cohere`            | `cohere`       | ✅   | ✅    | —     | —     | —      | ✅     | —      | —          |
| DeepSeek      | `provider/deepseek`          | `deepseek`     | ✅   | —     | —     | —     | —      | —      | —      | —          |
| Gemini        | `provider/gemini`            | `gemini`       | ✅   | ✅    | —     | —     | —      | —      | —      | —          |
| Groq          | `provider/groq`              | `groq`         | ✅   | —     | —     | —     | —      | —      | ✅     | —          |
| Mistral       | `provider/mistral`           | `mistral`      | ✅   | ✅    | —     | —     | —      | —      | —      | —          |
| Ollama        | `provider/ollama`            | `ollama`       | ✅   | ✅    | —     | —     | —      | —      | —      | —          |
| Perplexity    | `provider/perplexity`        | `perplexity`   | ✅   | —     | —     | —     | —      | —      | —      | —          |
| TogetherAI    | `provider/togetherai`        | `togetherai`   | ✅   | —     | ✅    | —     | —      | ✅     | —      | —          |
| xAI           | `provider/xai`               | `xai`          | ✅   | —     | ✅    | ✅    | —      | —      | —      | —          |

Notes:
- TogetherAI chat routes through the OpenAI-compatible path; its native provider
  implements image + rerank.
- Azure chat/embed/image come from one `provider/azure` provider.
- xAI implements chat + image + video from a single `*Provider`.
- `openaiobject` is a standalone object-generation backend (OpenAI Chat
  Completions with `response_format.json_schema`).

**Extended Thinking Support:** Anthropic provider supports Claude extended
thinking (`reasoning_effort`/`thinking_budget_tokens`) via
`chat.Request.ProviderOptions`.

This table reflects capabilities resolvable through the runtime. Additional
provider capabilities may be added as packages evolve against domain interface
contracts.

## UI Layer — Templ + Datastar

The UI layer maps the new AI SDK UI concepts to server-side Go using
[Templ](https://templ.guide)
for HTML templating and [Datastar](https://data-star.dev) for real-time
streaming reactivity via SSE.

### Key Concepts

| Concept            | Go Equivalent                          |
| ------------------- | -------------------------------------- |
| `useChat()` hook    | `chat.Chat` struct with methods        |
| `UIMessage`         | `chat.UIMessage` struct                |
| `ChatTransport`     | `chat.Transport` interface             |
| `sendMessage()`     | `Chat.Send(ctx, msg)` method           |
| `status` (reactive) | Datastar signals on the DOM            |
| `onToolCall`        | Callback registered on `ChatOptions`   |
| `addToolOutput()`   | `Chat.AddToolOutput(ctx, opts)` method |
| `onFinish`          | Callback registered on `ChatOptions`   |

### Component Strategy

Templ components are written as `.templ` files
using Datastar attributes for reactivity:

- `data-signals` for local state
- `data-on-*` for event handling
- SSE streaming for real-time text deltas from `streamText`

## File Organization

```tree
ai-sdk-examples/            # Example programs demonstrating SDK usage
  openai-chat/              #   Simple chat CLI with OpenAI
  anthropic-agent/          #   Agent with tool-use and streaming
  object-generation/        #   Structured object generation
  speech-to-text/           #   Audio transcription example
  image-generation/         #   Image generation example
  video-generation/         #   Video generation example
cmd/ai-sdk/                 # Entrypoint — wires dependencies, starts server
chat/                     # Domain: chat types & interface
embed/                    # Domain: embedding types & interface
image/                    # Domain: image generation types & interface
speech/                   # Domain: speech synthesis types & interface
transcribe/               # Domain: transcription types & interface
object/                   # Domain: structured object generation types & interface
video/                    # Domain: video generation types & interface
rerank/                   # Domain: reranking types & interface
core/                     # Services: GenerateText, StreamText orchestration
agent/                    # Agent: tool-loop agent over StreamText
runtime/                  # Provider resolution, catalog, provider classes
middleware/               # Middleware: wraps domain interfaces (logging, telemetry)
registry/                 # Infrastructure: provider registry
schema/                   # Infrastructure: JSON Schema builder
util/                     # Infrastructure: prompt helpers, tokeniser
upload/                   # Infrastructure: multipart form parsing
error/                    # Infrastructure: sentinel errors
logger/                   # Infrastructure: structured logging abstraction
telemetry/                # Infrastructure: OTel-compatible tracing interfaces
prompt/                   # Infrastructure: prompt manager
uimessage/                # UI: message protocol (chunks, SSE encoding)
  sse/                    #   SSE writer and stream processing
provider/                 # Providers: concrete implementations
  anthropic/
  azure/
  cohere/
  deepseek/
  gemini/
  groq/
  mistral/
  ollama/
  openai/
  openaiobject/
  perplexity/
  togetherai/
  xai/
ui/                       # UI: Templ components & HTTP handlers
  chat/                   #   Chat state management
  components/             #   Templ component files (.templ)
  handlers/               #   HTTP handler implementations
```

## Package Reference

### `runtime/` — AI Provider Runtime

The runtime layer resolves model references like `openai/gpt-5.4` into working
provider instances. It is designed for applications (such as `archied`) that
want to consume AI providers without hardcoding every implementation.

```tree
runtime/
  doc.go            Package-level documentation
  provider_class.go ProviderClass interface + class registry
  catalog.go        models.dev catalog loader + merge/overrides
  config.go         Declarative runtime configuration
  runtime.go        Runtime: Chat/ChatStream + per-domain entrypoints
  builtin.go        Built-in classes (openai-compatible, openai, anthropic, ...)
```

**Key abstractions:**

- `ProviderClass` — a factory that turns a `ProviderConfig` into a `ProviderSet`
  of domain providers. `ProviderSet` has one optional field per capability;
  `Has/Capability` reports what a set satisfies.
- `Catalog` — loads `https://models.dev/api.json`, merges overrides, and exposes
  provider/model metadata.
- `Runtime` — public entry point: `Chat`, `ChatStream`, plus `*Provider`
  resolvers and convenience methods for every domain.

**Trap:** A class must advertise a capability in its `caps` AND actually return a
provider that satisfies it. Advertising a capability whose builder returns a
provider that does not implement the interface leaves the `ProviderSet` field nil
(no half-wired set). When building a single provider that implements several
interfaces, prefer the combined builder + type-assertion so one instance is
shared.

### `image/` — Image Generation + Editing

```tree
image/
  client.go           Thin Client facade (GenerateImage + EditImage) with nil-guard
  doc.go              Package-level documentation
  errors.go           Sentinel errors (ErrNoProvider, ErrEditNotSupported, ...)
  provider.go         Provider interface (GenerateImage) + optional Editor interface
  provider_options.go Provider-specific options helpers
  types.go            GenerateImageRequest/Response, EditImageRequest/Response
```

**Key interfaces:**

- `Provider` — `GenerateImage(ctx, req)` (required)
- `Editor` — `EditImage(ctx, req)` (optional capability; type-assert to use, or
  call `image.Client.EditImage` which returns `ErrEditNotSupported` when the
  provider can't edit).

**Trap:** Keep image editing on the optional `Editor` interface, not on
`Provider`. Widening `Provider` forces every provider (azure, togetherai, xai)
and every middleware wrapper to implement the method, breaking the build for
non-editing backends.

### `video/` — Video Generation

```tree
video/
  client.go           Thin Client facade with nil-guard
  doc.go              Package-level documentation
  errors.go           Sentinel errors
  provider.go         Provider interface (GenerateVideo)
  types.go            GenerateVideoRequest, GenerateVideoResponse, VideoMode
```

**Key types:**

- `GenerateVideoRequest` — Model, Prompt, Duration, Resolution, FrameRate,
  plus typed `Mode` (`VideoModeTextToVideo`/`EditVideo`/`ExtendVideo`/
  `ReferenceToVideo`), `SourceVideo`, `ReferenceImages`, `Ratio`.
- `VideoResult` — Data, URL, MediaType. Async providers (e.g. xAI) poll
  internally inside `GenerateVideo`, so the call blocks until the video is ready
  or fails.

**Trap:** Preserve back-compat: providers should read typed mode/source/reference
fields first and fall back to legacy `ProviderOptions["xai"]` keys only when the
typed fields are unset. Do not add a competing `Duration` int-seconds field
alongside the existing string field — it creates ambiguity.

### `object/` — Object Generation Domain

```tree
object/
  client.go           Thin Client facade with nil-guard
  doc.go              Package-level documentation
  errors.go           Sentinel errors
  provider.go         Provider interface (GenerateObject + StreamObject)
  provider_options.go Provider-specific options helpers
  stream.go           ObjectStream iterator (Next/Close), ObjectChunk
  types.go            Request, Response, Object, ObjectResult
```

**Key types:**

- `Provider` interface — `GenerateObject(ctx, req)` + `StreamObject(ctx, req)`
- `Request` — Model, Prompt, MaxTokens, Schema, ProviderOptions
- `ObjectResult` — type alias for `any`; providers return `object.Object` (or a
  richer shape). `core.GenerateTypedObject[T]`/`StreamTypedObject[T]` derive the
  schema from `T` and strictly decode.

**Usage via core:**

```go
result, err := core.GenerateObject(ctx, provider, objRequest)
value, err := core.GenerateTypedObject[MyType](ctx, provider, objRequest)
```

**Trap:** A provider package must not import `core` (output would be an onion
violation). Validate `GenerateTypedObject` integration in a throwaway test
package and delete it, keeping the provider→core boundary clean.

### `agent/` — Agent Orchestration

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

### `upload/` — File Upload Utilities

```go
ParseMultipartForm(r *http.Request, maxMemory int64) ([]File, error)
DetectMediaType(data []byte) string  // PNG, JPEG, GIF, PDF detection
ToBase64(f File) string
```

### `util/` — Prompt Helpers and Token Counting

```go
util.SystemPrompt("You are a helpful assistant.")
util.UserPrompt("What is the weather?")
util.AssistantPrompt("Let me check that for you.")
util.ToolResultMessage(callID, result)
util.FormatMessages(messages) // human-readable formatting
```

### `error/` — Sentinel Errors

```go
ErrInvalidInput           = errors.New("invalid input")
ErrTimeout                = errors.New("timeout")
ErrCancelled              = errors.New("cancelled")
ErrNotImplemented         = errors.New("not implemented")
ErrProviderNotAvailable   = errors.New("provider not available")
ErrModelNotFound          = errors.New("model not found")
ErrQuotaExceeded          = errors.New("quota exceeded")
```

### `logger/` — Structured Logging

- `Logger` interface — `Info(msg, attrs...), Error(msg, attrs...), Debug(msg, attrs...)`
- `NewSlogLogger(l *slog.Logger) Logger` — adapts stdlib slog
- `NoopLogger` — no-op implementation for tests

### `telemetry/` — OpenTelemetry-Compatible Tracing

```go
type Span interface {
    End()
    SetAttribute(key, value string)
    RecordError(err error)
}
type Tracer interface {
    Start(ctx context.Context, name string) (context.Context, Span)
}
```

`NoopSpan` / `NoopTracer` are zero-cost no-ops; `DefaultTracer` is the package
fallback.

### `middleware/` — Provider Middleware

```go
type ChatMiddleware func(next chat.Provider) chat.Provider
func Chain(middlewares ...ChatMiddleware) ChatMiddleware // composes left-to-right
```

- `TelemetryMiddleware` — wraps provider with OTel spans for Chat/ChatStream
- `ChatRequestHook` / `ChatResponseHook` — interception points
- Generation domains have per-domain wrappers (`retry_image`,
  `telemetry_video`, `circuitbreaker_object`, `rerank`, etc.); image editing has
  `*_image_edit` wrappers.

### `uimessage/sse/` — SSE Streaming

- `Writer` — streams `uimessage.Chunk` values as SSE `data:` events
- `NewWriter(rw http.ResponseWriter)` — applies headers, flushes automatically
- `Headers` — canonical AI SDK UI stream headers
  (`X-Vercel-Ai-Ui-Message-Stream: v1`)
- `Pipe(ctx, src, w)` — drains chunk channel into SSE writer
- `FromTextStream` — adapts core text stream into UI message chunks

## Build & Test

Commands are defined in `Taskfile.yaml` (Go 1.26.2, `gofumpt`, `goimports`,
`golangci-lint`, `staticcheck`, `go vet`, `deadcode`).

```bash
task check        # gofumpt -w . + go vet + staticcheck + golangci-lint + deadcode + go test ./...
                  #   ⚠ THE DEFINITIVE GATE — run this before calling a change done
task test         # go test ./...
task test:race    # go test -race ./...
task test:cover   # go test -coverprofile=coverage.out ./...
task fmt          # gofumpt -w . && goimports -w .
task vet          # go vet ./...
task lint         # golangci-lint run ./...
task staticcheck  # staticcheck ./...
task deadcode     # golangci-lint run --tests=false --enable-only=unused,staticcheck ./...
task tidy         # go mod tidy (root + ai-sdk-examples)
task clean        # remove coverage.out
```

Single package/test runs:

```bash
go test ./runtime/... -run TestProviderSetHas -v -count=1
```

## Repository Hygiene

- **Generated assets are LAW:** Never hand-edit, revert, or fight generated
  files — `.templ` output (`*_templ.go`), schema outputs, and any `go generate`
  artifact. Re-run the generator and commit the result verbatim.
- **Ignored paths:** Never commit build artifacts, binaries, `coverage.out`,
  `.serena/`, `.beads/`, or IDE/editor files.
- **Scratch files:** Place scratch files strictly in `/tmp` or the harness
  scratch space. Never put scratch files in the working tree.
- **Conventional Commits:** Scope by package — `feat(runtime): ...`,
  `feat(image): ...`, `fix(xai): ...`, `chore(docs): ...`. One logical change per
  commit; discrete features get separate commits.
- **Commit policy:** Commit finished, gate-clean changes (`task check` passing)
  **without asking**. This repo works in branch/worktree; commit
  each validated slice so nothing is left uncommitted. Push only when instructed.

## Development Protocol

1. **Red (Failing Test):** Write failing test cases first using table-driven tests
   against target behaviour. Verify the failure originates from test assertions,
   not compilation errors.
2. **Green (Implementation):** Implement minimal code to satisfy the failing
   tests.
3. **Quality Gate:** Run `task check`.
4. **Formatting is LAW:** Adopt all formatting and simplification changes from
   `task fmt` (`gofumpt` + `goimports`) verbatim. Never revert or fight canonical
   linter/formatter diffs.
5. **Linter Guard:** When fixing lint findings, preserve the existing semantics
   (e.g. keep boolean predicates alongside `errors.As`; don't duplicate slice
   elements when extracting loops). Do not break text-generation paths while
   changing a provider's error handling.

## Issue Tracking (Beads)

All task management is tracked via **bd (beads)**. Do not write local Markdown
TODO lists or use non-Beads trackers.

```bash
bd ready               # List unclaimed work
bd show <id>           # View issue details
bd update <id> --claim # Claim an issue
bd close <id>          # Mark issue complete
bd remember            # Persist cross-session architectural facts
```

`bd prime` injects full workflow context. Use `bd remember` for knowledge
retention. Issues live in a local Dolt DB; `.beads/issues.jsonl` is an export.

## Session Completion Protocol

1. **Log remaining work:** File new items via `bd` for identified debt or
   follow-up tasks.
2. **Run gate:** Verify `task check` passes completely clean.
3. **Update tracker:** Close finished issues via `bd close <id>`.
4. **Commit (always):**

   ```bash
   git add <scoped-files>
   git commit -m "feat(scope): description"
   ```

   Commit each validated slice as you go — do not leave work uncommitted.
5. **Sync & Push Policy:** Push to git remotes and run `bd dolt push` only when
   explicitly instructed.
6. **Handoff:** Report changed files, gate verification results, and active
   issue states.

## Examples

Example programs demonstrating SDK usage live in `ai-sdk-examples/`:

| Example              | Description                                       |
| -------------------- | ------------------------------------------------- |
| `openai-chat/`       | Interactive chat CLI using OpenAI provider        |
| `anthropic-agent/`   | Agent with mock weather tool and streaming output |
| `object-generation/` | Structured object generation API pattern          |
| `speech-to-text/`    | Audio transcription API pattern                   |
| `image-generation/`  | Image generation API pattern (TogetherAI)         |
| `video-generation/`  | Video generation API pattern (xAI)                |

Run examples from the workspace root:

```bash
# Openai chat
OPENAI_API_KEY=sk-... go run ./ai-sdk-examples/openai-chat/

# Anthropic agent with tool use
ANTHROPIC_API_KEY=sk-ant-... go run ./ai-sdk-examples/anthropic-agent/ "What is the weather in London?"

# Informational examples (no API key needed)
go run ./ai-sdk-examples/object-generation/
go run ./ai-sdk-examples/speech-to-text/
go run ./ai-sdk-examples/image-generation/
go run ./ai-sdk-examples/video-generation/
```

## References

- Project structure: <https://templ.guide/project-structure/project-structure>
- AI SDK Core: <https://ai-sdk.dev/docs/reference/ai-sdk-core>
- AI SDK UI useChat: <https://ai-sdk.dev/docs/reference/ai-sdk-core/ui-message>
- Datastar: <https://data-star.dev>
