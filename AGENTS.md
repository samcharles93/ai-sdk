# AGENTS.md — AI SDK (Go re-interpretation)

---

## CORE | STRICT — Onion Model Architecture

This project follows the **onion model** (also known as hexagonal / ports &
adapters).  
Each layer is responsible for a specific concern and **MUST NOT** know about any
layer above it.

Dependency direction: **inward only** — outer layers depend on inner layers,
never the reverse.

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
│  ─────────────── image/                       │  Image gen types + Provider
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
   `core`, `ui`, `registry`, or `middleware`.
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

### Dependency Injection

- Every struct has a `New` constructor accepting its dependencies as interfaces.
- No global state. No package-level singletons. No `init()` for wiring.
- Changing a `New` signature produces compile-time errors showing all affected
  call sites.

---

## UI Layer — Templ + Datastar

The AI SDK UI layer ports the concepts from the JS AI SDK UI libraries
(`useChat`, `Chat`, etc.)  
to server-side Go using [Templ](https://templ.guide) for HTML templating and
[Datastar](https://data-star.dev)  
for real-time streaming reactivity via SSE.

### Key Concepts (ported from JS)

| JS Concept          | Go Equivalent                          |
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

Templ components from the JS component libraries are ported as `.templ` files  
using Datastar attributes for reactivity:

- `data-signals` for local state
- `data-on-*` for event handling
- SSE streaming for real-time text deltas from `streamText`

---

## File Organization

```tree
ai-sdk-examples/            # Example programs demonstrating SDK usage
  openai-chat/              #   Simple chat CLI with OpenAI
  anthropic-agent/          #   Agent with tool-use and streaming
  object-generation/        #   Structured object generation
  speech-to-text/           #   Audio transcription example
  image-generation/         #   Image generation example
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
  perplexity/
  togetherai/
  xai/
ui/                       # UI: Templ components & HTTP handlers
  chat/                   #   Chat state management
  components/             #   Templ component files (.templ)
  handlers/               #   HTTP handler implementations
```

## Provider Ecosystem

| Provider   | Package                   | Chat | Embed | Image | Speech | Transcribe | Object | Rerank | Video |
| ---------- | ------------------------- | ---- | ----- | ----- | ------ | ---------- | ------ | ------ | ----- |
| OpenAI     | `provider/openai`     | ✅   | —     | —     | —      | —          | —      | —      | —     |
| Anthropic  | `provider/anthropic`  | ✅   | —     | —     | —      | —          | —      | —      | —     |
| Azure      | `provider/azure`      | ✅   | ✅    | ✅    | —      | —          | —      | —      | —     |
| Cohere     | `provider/cohere`     | ✅   | ✅    | —     | —      | —          | —      | ✅     | —     |
| DeepSeek   | `provider/deepseek`   | ✅   | —     | —     | —      | —          | —      | —      | —     |
| Gemini     | `provider/gemini`     | ✅   | ✅    | —     | —      | —          | —      | —      | —     |
| Groq       | `provider/groq`       | ✅   | —     | —     | —      | —          | —      | —      | —     |
| Mistral    | `provider/mistral`    | ✅   | ✅    | —     | —      | —          | —      | —      | —     |
| Ollama     | `provider/ollama`     | ✅   | ✅    | —     | —      | —          | —      | —      | —     |
| Perplexity | `provider/perplexity` | ✅   | —     | —     | —      | —          | —      | —      | —     |
| TogetherAI | `provider/togetherai` | ✅   | ✅    | ✅    | —      | —          | —      | —      | —     |
| xAI        | `provider/xai`        | ✅   | —     | —     | —      | —          | —      | —      | —     |

**Extended Thinking Support:** Anthropic provider supports Claude extended
thinking (`reasoning_effort`/`thinking_budget_tokens`) via
`chat.Request.ProviderOptions`.

This table reflects currently implemented interfaces in this repository.
Additional provider capabilities may be added as packages evolve against domain
interface contracts.

## New Package Documentation

### `runtime/` — AI Provider Runtime

The runtime layer resolves model references like `openai/gpt-5.4` into working
provider instances. It is designed for applications (such as `tau`) that want to
consume AI providers without hardcoding every implementation.

```tree
runtime/
  doc.go            Package-level documentation
  provider_class.go ProviderClass interface + class registry
  catalog.go        models.dev catalog loader + merge/overrides
  config.go         Declarative runtime configuration
  runtime.go        Runtime: Chat, ChatStream, provider resolution
  builtin.go        Built-in classes (openai-compatible, openai, anthropic, ...)
```

**Key abstractions:**

- `ProviderClass` — a factory that turns a `ProviderConfig` into a `ProviderSet`
  of domain providers. Built-in classes include `openai-compatible` (any
  OpenAI-compatible endpoint) and the known models.dev npm mappings (`openai`,
  `anthropic`, `groq`, ...).
- `Catalog` — loads `https://models.dev/api.json`, merges overrides, and exposes
  provider/model metadata.
- `Runtime` — public entry point: `Chat(ctx, "provider/model", opts)` and
  `ChatStream(ctx, "provider/model", opts)`.

**Extensibility:**

```go
runtime.RegisterClass(myCustomClass{})
```

Custom classes can perform arbitrary setup (discovery, auth exchange, header
injection) before returning domain providers. This is the escape hatch for
providers like OpenShift MaaS that are not directly covered by the built-in
classes.

### `object/` — Object Generation Domain

The object generation domain provides types and interfaces for structured JSON
output from language models. It mirrors the AI SDK's `generateObject` function.

```tree
object/
  client.go           Thin Client facade with nil-guard
  doc.go              Package-level documentation
  errors.go           Sentinel errors (ErrNoProvider, ErrInvalidRequest)
  provider.go         Provider interface (GenerateObject method)
  provider_options.go Provider-specific options helpers
  types.go            Request, Response, Object, ObjectResult types
```

**Key types:**

- `Provider` interface — `GenerateObject(ctx, req) (ObjectResult, error)`
- `Request` — Model, Prompt, MaxTokens, ProviderOptions
- `Response` — ID, Model, Object, Warnings
- `ObjectResult` — type alias for `any`; providers return concrete types

**Usage via core:**

```go
result, err := core.GenerateObject(ctx, provider, objRequest)
```

### `video/` — Video Generation Domain

Types and interfaces for video generation from text prompts.

```tree
video/
  client.go           Thin Client facade with nil-guard
  doc.go              Package-level documentation
  errors.go           Sentinel errors
  provider.go         Provider interface (GenerateVideo method)
  types.go            GenerateVideoRequest, GenerateVideoResponse, VideoResult
```

**Key types:**

- `GenerateVideoRequest` — Model, Prompt, Duration, Resolution, FrameRate
- `GenerateVideoResponse` — Videos ([]VideoResult), Warnings
- `VideoResult` — Data, URL, MediaType

### `agent/` — Agent Orchestration

The agent package provides a tool-loop agent that orchestrates multi-step
reasoning and tool execution over `core.StreamText`.

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
- `RunAgent(ctx, provider, prompt, tools, maxSteps)` — convenience function
- `StreamEvent` — Type-based event dispatch (TextDelta, ToolCall, ToolResult,
  etc.)

**Event system:**

```go
switch ev.Type {
case agent.EventTextDelta:   // streaming text
case agent.EventToolCall:    // tool invocation requested
case agent.EventToolResult:  // tool execution complete
case agent.EventFinish:      // generation complete
case agent.EventError:       // stream error
case agent.EventAbort:       // context cancelled
}
```

The agent does NOT execute tools itself — `core.StreamText` handles the full
tool loop internally. The agent concentrates on event translation and lifecycle
management.

### `upload/` — File Upload Utilities

Parses multipart form data and provides file type detection.

```tree
upload/
  doc.go              Package-level documentation
  skill.go            Skill-specific upload helpers
  upload.go           ParseMultipartForm, DetectMediaType, ToBase64
  upload_test.go      Tests
```

**Key functions:**

- `ParseMultipartForm(r *http.Request, maxMemory int64) ([]File, error)`
- `DetectMediaType(data []byte) string` — PNG, JPEG, GIF, PDF detection
- `ToBase64(f File) string`

**File type:**

```go
type File struct {
    Name      string
    Data      []byte
    MediaType string
    Size      int64
}
```

### `util/` — Prompt Helpers and Token Counting

Shared utilities for prompt construction and token estimation.

```tree
util/
  doc.go              Package-level documentation
  id.go               ID generation
  prompt.go           FormatMessages, SystemPrompt, UserPrompt, etc.
  prompt_test.go      Tests
  stream.go           Stream utilities
  tokenizer.go        Token counting helpers
  tokenizer_test.go   Tests
```

**Prompt construction:**

```go
util.SystemPrompt("You are a helpful assistant.")
util.UserPrompt("What is the weather?")
util.AssistantPrompt("Let me check that for you.")
util.ToolResultMessage(callID, result)
util.FormatMessages(messages) // human-readable formatting
```

### `error/` — Sentinel Errors

Package-level sentinel error values for use across the project.

```tree
error/
  errors.go           Sentinel error variables
  errors_test.go      Tests
```

**Sentinel errors:**

```go
ErrInvalidInput      = errors.New("invalid input")
ErrTimeout           = errors.New("timeout")
ErrCancelled         = errors.New("cancelled")
ErrNotImplemented    = errors.New("not implemented")
ErrProviderNotAvailable = errors.New("provider not available")
ErrModelNotFound     = errors.New("model not found")
ErrQuotaExceeded     = errors.New("quota exceeded")
```

### `logger/` — Structured Logging

Minimal structured logging abstraction. Adaptable to `log/slog`.

```tree
logger/
  logger.go           Logger interface, slogLogger adapter, NoopLogger
  logger_test.go      Tests
```

**Key types:**

- `Logger` interface —
  `Info(msg, attrs...), Error(msg, attrs...), Debug(msg, attrs...)`
- `NewSlogLogger(l *slog.Logger) Logger` — adapts stdlib slog
- `NoopLogger` — no-op implementation for tests

### `telemetry/` — OpenTelemetry-Compatible Tracing

Minimal tracing interfaces compatible with OpenTelemetry conventions.

```tree
telemetry/
  doc.go              Package-level documentation
  telemetry.go        Span, Tracer interfaces, NoopSpan, NoopTracer
```

**Key interfaces:**

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

- `NoopSpan` / `NoopTracer` — zero-cost no-op implementations
- `DefaultTracer` — package-level `NoopTracer{}` fallback

### `middleware/` — Provider Middleware

Middleware layer wrapping domain Provider interfaces. Supports composition via
`Chain()`.

```tree
middleware/
  doc.go              Package-level documentation
  middleware.go       ChatMiddleware type, ChatRequestHook, ChatResponseHook, Chain()
  telemetry.go        TelemetryMiddleware (spans Chat/ChatStream calls)
  telemetry_test.go   Tests
```

**Key patterns:**

- `ChatMiddleware func(next chat.Provider) chat.Provider`
- `Chain(middlewares ...ChatMiddleware) ChatMiddleware` — composes left-to-right
- `TelemetryMiddleware` — wraps provider with OTel spans for Chat and ChatStream
- `ChatRequestHook` / `ChatResponseHook` — interception points

### `uimessage/sse/` — SSE Streaming

Server-Sent Events wire format for the AI SDK UI message stream protocol.

```tree
uimessage/sse/
  sse_test.go         Tests
  transform.go        Core text-stream to chunk channel adaptation
  writer.go           SSE Writer, Headers, Pipe
```

**Key components:**

- `Writer` — streams `uimessage.Chunk` values as SSE `data:` events
- `NewWriter(rw http.ResponseWriter)` — applies headers, flushes automatically
- `Headers` — canonical AI SDK UI stream headers
  (`X-Vercel-Ai-Ui-Message-Stream: v1`)
- `Pipe(ctx, src, w)` — drains chunk channel into SSE writer
- `FromTextStream` — adapts core text stream into UI message chunks

## Examples

Example programs demonstrating SDK usage live in `ai-sdk-examples/`:

| Example              | Description                                       |
| -------------------- | ------------------------------------------------- |
| `openai-chat/`       | Interactive chat CLI using OpenAI provider        |
| `anthropic-agent/`   | Agent with mock weather tool and streaming output |
| `object-generation/` | Structured object generation API pattern          |
| `speech-to-text/`    | Audio transcription API pattern                   |
| `image-generation/`  | Image generation API pattern (Azure, TogetherAI)  |

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
```

## References

- Project structure: <https://templ.guide/project-structure/project-structure>
- AI SDK Core: <https://ai-sdk.dev/docs/reference/ai-sdk-core>
- AI SDK UI useChat: <https://ai-sdk.dev/docs/reference/ai-sdk-core/ui-message>
- Datastar: <https://data-star.dev>

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:46cd31e7 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/core-concepts/sync-concepts.md for details and anti-patterns.

## Agent Context Profiles

The managed Beads block is task-tracking guidance, not permission to override repository, user, or orchestrator instructions.

- **Conservative (default)**: Use `bd` for task tracking. Do not run git commits, git pushes, or Dolt remote sync unless explicitly asked. At handoff, report changed files, validation, and suggested next commands.
- **Minimal**: Keep tool instruction files as pointers to `bd prime`; use the same conservative git policy unless active instructions say otherwise.
- **Team-maintainer**: Only when the repository explicitly opts in, agents may close beads, run quality gates, commit, and push as part of session close. A current "do not commit" or "do not push" instruction still wins.

## Session Completion

This protocol applies when ending a Beads implementation workflow. It is subordinate to explicit user, repository, and orchestrator instructions.

1. **File issues for remaining work** - Create beads for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **Handle git/sync by active profile**:
   ```bash
   # Conservative/minimal/default: report status and proposed commands; wait for approval.
   git status

   # Team-maintainer opt-in only, unless current instructions forbid it:
   git pull --rebase
   bd dolt push
   git push
   git status
   ```
5. **Hand off** - Summarize changes, validation, issue status, and any blocked sync/commit/push step

**Critical rules:**
- Explicit user or orchestrator instructions override this Beads block.
- Do not commit or push without clear authority from the active profile or the current user request.
- If a required sync or push is blocked, stop and report the exact command and error.
<!-- END BEADS INTEGRATION -->

<!-- BEGIN BEADS CODEX SETUP: generated by bd setup codex -->
## Beads Issue Tracker

Use Beads (`bd`) for durable task tracking in repositories that include it. Use the `beads` skill at `.agents/skills/beads/SKILL.md` (project install) or `~/.agents/skills/beads/SKILL.md` (global install) for Beads workflow guidance, then use the `bd` CLI for issue operations.

### Quick Reference

```bash
bd ready                # Find available work
bd show <id>            # View issue details
bd update <id> --claim  # Claim work
bd close <id>           # Complete work
bd prime                # Refresh Beads context
```

### Rules

- Use `bd` for all task tracking; do not create markdown TODO lists.
- Run `bd prime` when Beads context is missing or stale. Codex 0.129.0+ can load Beads context automatically through native hooks; use `/hooks` to inspect or toggle them.
- Keep persistent project memory in Beads via `bd remember`; do not create ad hoc memory files.

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/core-concepts/sync-concepts.md for details and anti-patterns.
<!-- END BEADS CODEX SETUP -->
